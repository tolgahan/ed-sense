package backend

import (
	"net"
	"time"

	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
)

// Kind is a controller app EDSense drives the controller through, by its
// name in the settings file.
type Kind string

const (
	KindDSX        Kind = "dsx"
	KindDS4Windows Kind = "ds4windows"
)

// DSXExe is DSX's process.
const DSXExe = "DSX.exe"

// ProbeTimeout is how long a probe waits for an answer.
const ProbeTimeout = 300 * time.Millisecond

// DetectEnv is how Detect sees the system; tests give their own.
type DetectEnv struct {
	Running    func(exe string) bool                       // a process runs, by its exe's name
	DS4Window  func() bool                                 // DS4Windows' window is there, whatever its exe is called; nil: never
	DS4Version func() string                               // the running DS4Windows' version, "" when unknown; nil: unknown
	Probe      func(addr *net.UDPAddr) (dsx.Dialect, bool) // who answers a GetDSXStatus
	DSXAddr    *net.UDPAddr
	DS4Addr    *net.UDPAddr // DS4Windows' listener
}

// ds4Running: DS4Windows runs, by its exe's name or by its window (a
// renamed exe, which DS4Windows offers for games that block it).
func (env DetectEnv) ds4Running() bool {
	return env.Running(ds4w.Exe) || env.DS4Window != nil && env.DS4Window()
}

// Detect picks the controller app for "auto", and says why: the one that
// runs; with both running, the one that answers; with neither, DS4Windows
// only when it answers on its port, else DSX. A DS4Windows before 5 has no
// DSX listener, so it counts as not running.
func Detect(env DetectEnv) (Kind, string) {
	kind, why, _ := DetectSure(env)
	return kind, why
}

// DetectSure is Detect, and whether its pick is sure: exactly one app runs
// (a DS4Windows before 5 does not count), or neither runs and DS4Windows
// answers on its port, or both run and one answers. The fallbacks, DSX
// when neither runs or when neither answers, are not.
func DetectSure(env DetectEnv) (Kind, string, bool) {
	dsxOn, ds4On := env.Running(DSXExe), env.ds4Running()
	old := ""
	if ds4On && env.DS4Version != nil {
		if v := env.DS4Version(); v != "" {
			if major, ok := ds4w.Major(v); ok && major < 5 {
				ds4On, old = false, v
			}
		}
	}
	switch {
	case dsxOn && !ds4On:
		return KindDSX, "DSX runs", true
	case ds4On && !dsxOn:
		return KindDS4Windows, "DS4Windows runs", true
	case !dsxOn && !ds4On:
		// a DS4Windows this process cannot see still answers on its port
		if d, ok := env.Probe(env.DS4Addr); ok && d == dsx.DS4Windows {
			return KindDS4Windows, "DS4Windows answers on its port", true
		}
		if old != "" {
			return KindDSX, "DS4Windows " + old + " has no DSX listener, and DSX does not run yet", false
		}
		return KindDSX, "neither DSX nor DS4Windows runs yet", false
	}
	d, ok := env.Probe(env.DS4Addr)
	switch {
	case ok && d == dsx.DS4Windows:
		return KindDS4Windows, "both run, DS4Windows answers", true
	case ok && d == dsx.DSX && env.DS4Addr.String() == env.DSXAddr.String():
		return KindDSX, "both run, DSX answers", true
	}
	if d, ok := env.Probe(env.DSXAddr); ok && d == dsx.DSX {
		return KindDSX, "both run, DSX answers", true
	}
	return KindDSX, "both run, neither answers yet", false
}

// AppWatch follows which controller apps run, for "auto": Check looks at
// the processes and DS4Windows' window, and probes and reads DS4Windows'
// version only when they changed. It is for one goroutine at a time.
type AppWatch struct {
	Env DetectEnv
	// Before, when set, runs before each detection, that is when the apps
	// that run changed, and may update Env: the addresses to probe, which
	// a DS4Windows that started since may have moved.
	Before func(env *DetectEnv)
	last   [3]bool
	seen   bool
}

// Check returns the app to use and why, and false when the apps that run
// are the same as at the last check.
func (w *AppWatch) Check() (Kind, string, bool) {
	kind, why, _, changed := w.CheckDetail()
	return kind, why, changed
}

// CheckDetail is Check, and whether the pick is sure (DetectSure).
func (w *AppWatch) CheckDetail() (kind Kind, why string, sure, changed bool) {
	now := [3]bool{w.Env.Running(DSXExe), w.Env.Running(ds4w.Exe), w.Env.DS4Window != nil && w.Env.DS4Window()}
	if w.seen && now == w.last {
		return "", "", false, false
	}
	w.seen, w.last = true, now
	if w.Before != nil {
		w.Before(&w.Env)
	}
	kind, why, sure = DetectSure(w.Env)
	return kind, why, sure, true
}

// Forget makes the next check count as a change, so it detects again: for
// a switch that could not be made yet.
func (w *AppWatch) Forget() { w.seen = false }
