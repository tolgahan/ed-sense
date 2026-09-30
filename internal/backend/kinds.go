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
		return KindDSX, "DSX runs"
	case ds4On && !dsxOn:
		return KindDS4Windows, "DS4Windows runs"
	case !dsxOn && !ds4On:
		// a DS4Windows this process cannot see still answers on its port
		if d, ok := env.Probe(env.DS4Addr); ok && d == dsx.DS4Windows {
			return KindDS4Windows, "DS4Windows answers on its port"
		}
		if old != "" {
			return KindDSX, "DS4Windows " + old + " has no DSX listener, and DSX does not run yet"
		}
		return KindDSX, "neither DSX nor DS4Windows runs yet"
	}
	d, ok := env.Probe(env.DS4Addr)
	switch {
	case ok && d == dsx.DS4Windows:
		return KindDS4Windows, "both run, DS4Windows answers"
	case ok && d == dsx.DSX && env.DS4Addr.String() == env.DSXAddr.String():
		return KindDSX, "both run, DSX answers"
	}
	if d, ok := env.Probe(env.DSXAddr); ok && d == dsx.DSX {
		return KindDSX, "both run, DSX answers"
	}
	return KindDSX, "both run, neither answers yet"
}

// AppWatch follows which controller apps run, for "auto": Check looks at
// the processes and DS4Windows' window, and probes and reads DS4Windows'
// version only when they changed.
type AppWatch struct {
	Env  DetectEnv
	last [3]bool
	seen bool
}

// Check returns the app to use and why, and false when the apps that run
// are the same as at the last check.
func (w *AppWatch) Check() (Kind, string, bool) {
	now := [3]bool{w.Env.Running(DSXExe), w.Env.Running(ds4w.Exe), w.Env.DS4Window != nil && w.Env.DS4Window()}
	if w.seen && now == w.last {
		return "", "", false
	}
	w.seen, w.last = true, now
	kind, why := Detect(w.Env)
	return kind, why, true
}
