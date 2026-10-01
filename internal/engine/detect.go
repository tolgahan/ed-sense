package engine

import (
	"net"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
)

// Detection is what runs and who answers where, for the Controller page
// and the first run.
type Detection struct {
	T    int64 // when, in Unix milliseconds
	DSX  DSXSeen
	DS4W DS4WSeen
	Auto AutoPick
}

// DSXSeen is DSX as a detection saw it. Answers is who answers on its
// port: "dsx", "ds4windows" or "".
type DSXSeen struct {
	Running       bool
	Addr, Answers string
}

// DS4WSeen is DS4Windows as a detection saw it. Dir: its settings were
// found.
type DS4WSeen struct {
	Running                bool
	Version, Addr, Answers string
	Dir                    bool
}

// AutoPick is what Auto would pick now, and whether that is sure.
type AutoPick struct {
	Kind backend.Kind
	Why  string
	Sure bool
}

// Detect looks at the apps with the settings file's ports. A detection is
// kept for 2 s; fresh asks for a new one, at most one a second. It probes,
// so it takes up to a few hundred milliseconds: never on the loop.
func (e *Engine) Detect(fresh bool) Detection {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	if !e.detAt.IsZero() {
		age := time.Since(e.detAt)
		if age < time.Second || !fresh && age < 2*time.Second {
			return e.det
		}
	}
	e.det, e.detAt = e.detect(), time.Now()
	return e.det
}

func (e *Engine) detect() Detection {
	cfg := e.store.Snapshot().Config
	env, at := e.where(&cfg)
	answers := probeAll(env.Probe, env.DSXAddr, env.DS4Addr)
	seen := env
	seen.Probe = func(addr *net.UDPAddr) (dsx.Dialect, bool) {
		a := answers[addr.String()]
		return a.d, a.ok
	}
	d := Detection{T: time.Now().UnixMilli()}
	d.DSX = DSXSeen{Running: env.Running(backend.DSXExe), Addr: env.DSXAddr.String(), Answers: answers[env.DSXAddr.String()].name()}
	d.DS4W = DS4WSeen{Running: env.Running(ds4w.Exe) || env.DS4Window != nil && env.DS4Window(),
		Addr: env.DS4Addr.String(), Answers: answers[env.DS4Addr.String()].name(), Dir: at.ds4Dir != ""}
	if d.DS4W.Running && env.DS4Version != nil {
		d.DS4W.Version = env.DS4Version()
	}
	d.Auto.Kind, d.Auto.Why, d.Auto.Sure = backend.DetectSure(seen)
	return d
}

// answer is who answered a probe.
type answer struct {
	d  dsx.Dialect
	ok bool
}

func (a answer) name() string {
	switch {
	case !a.ok:
		return ""
	case a.d == dsx.DS4Windows:
		return string(backend.KindDS4Windows)
	}
	return string(backend.KindDSX)
}

// probeAll probes each address once, all at the same time.
func probeAll(probe func(*net.UDPAddr) (dsx.Dialect, bool), addrs ...*net.UDPAddr) map[string]answer {
	got := map[string]answer{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	asked := map[string]bool{}
	for _, addr := range addrs {
		key := addr.String()
		if asked[key] {
			continue
		}
		asked[key] = true
		wg.Go(func() {
			d, ok := probe(addr)
			mu.Lock()
			got[key] = answer{d, ok}
			mu.Unlock()
		})
	}
	wg.Wait()
	return got
}
