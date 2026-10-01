package engine

import (
	"errors"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
)

// watchApps is Auto: every few seconds, while the choice is auto, it looks
// at the apps that run until stop is closed (EDSense quits).
func (e *Engine) watchApps(stop <-chan struct{}) {
	tick := time.NewTicker(e.every)
	defer tick.Stop()
	aw := &autoWatch{e: e}
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		aw.check(stop)
	}
}

// ended: ch is closed.
func ended(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// autoWatch is Auto's state between two looks at the apps.
type autoWatch struct {
	e       *Engine
	w       *backend.AppWatch // nil while the choice is not auto
	waiting backend.Kind      // told that a switch to it waits
}

// check looks at the apps once. It switches only when the apps that run
// changed, the pick is another app than the one EDSense runs on, and the
// pick is sure; then only while EDSense does not drive the controller. A
// fallback (neither app runs, neither answers, an old DS4Windows) never
// switches. Once stop is closed (EDSense quits) it does nothing.
func (aw *autoWatch) check(stop <-chan struct{}) {
	e := aw.e
	if !e.op.TryLock() {
		return // an Apply or a choice runs; the next look sees what it did
	}
	defer e.op.Unlock()
	e.mu.Lock()
	a, auto, closed, applied, cur := e.a, e.choice == config.BackendAuto, e.closed, e.applied, e.cur
	e.mu.Unlock()
	if !auto || closed || a == nil {
		aw.w, aw.waiting = nil, ""
		return
	}
	if ended(stop) || a.Busy() {
		return // the apps are looked at again once it is idle
	}
	if aw.w == nil {
		env, _ := e.where(&applied)
		aw.w = &backend.AppWatch{Env: env, Before: e.readdress}
	}
	kind, why, sure, changed := aw.w.CheckDetail()
	if !changed {
		return
	}
	if kind == cur.Kind {
		aw.waiting = ""
		if sure {
			e.mu.Lock()
			e.why = "auto: " + why
			e.mu.Unlock()
			if kind == backend.KindDSX {
				e.dsxSure.Store(true)
			}
			e.changed()
		}
		return
	}
	if !sure {
		log.Printf("Controller app: %s would be used (%s), keeping %s", nameOf(kind), why, cur.Name)
		return
	}
	if ended(stop) {
		return // EDSense quit while the apps were probed
	}
	_, at := e.where(&applied)
	t := Target{Kind: kind, Why: "auto: " + why, Sure: true,
		DSXPort: at.dsxPort, DS4Addr: at.ds4Addr, DS4Dir: at.ds4Dir, DS4Port: applied.DS4WindowsPort}
	b, err := e.open(t, t.Why)
	if err != nil {
		return
	}
	// the game as the session knows it stays: Auto changes no folder, and
	// the session is built from the settings applied, so a poll_ms that
	// waits in the file keeps waiting
	keys := app.KeysOf(&applied)
	if err := a.Restart(b, true, true, &keys); err != nil {
		e.drop(b)
		if errors.Is(err, app.ErrBusy) {
			aw.w.Forget() // the next idle look tries again
			if aw.waiting != kind {
				log.Printf("Controller app: %s runs now; EDSense switches once it is not driving the controller", b.Name)
				aw.waiting = kind
			}
		}
		return
	}
	aw.waiting = ""
	e.mu.Lock()
	pinned := e.pinned
	e.mu.Unlock()
	e.took(a, b, t, applied, config.BackendAuto, pinned, nil)
}

// readdress points env at where the apps listen now, with the settings
// applied: a DS4Windows that started since may listen elsewhere.
func (e *Engine) readdress(env *backend.DetectEnv) {
	e.mu.Lock()
	cfg := e.applied
	e.mu.Unlock()
	fresh, _ := e.where(&cfg)
	env.DSXAddr, env.DS4Addr = fresh.DSXAddr, fresh.DS4Addr
}

// watchStore follows the settings file until done is closed: State's
// pending keys change with it, and a restart key that changed is named in
// the log.
func (e *Engine) watchStore(done <-chan struct{}) {
	wake, stop := e.store.Watch()
	defer stop()
	last := e.store.Snapshot()
	for {
		select {
		case <-done:
			return
		case <-wake:
		}
		snap := e.store.Snapshot()
		e.sayPending(last, snap)
		last = snap
		e.changed()
	}
}

// sayPending names the restart keys that changed from old to snap and now
// wait to be applied.
func (e *Engine) sayPending(old, snap *config.Snapshot) {
	if old.Rev == snap.Rev {
		return
	}
	changed := config.RestartKeys(&old.Config, &snap.Config)
	if len(changed) == 0 {
		return
	}
	e.mu.Lock()
	pending := e.pending(&snap.Config)
	e.mu.Unlock()
	changed = slices.DeleteFunc(changed, func(k string) bool { return !slices.Contains(pending, k) })
	if len(changed) > 0 {
		log.Printf("%s: changed; they apply with Apply now in the EDSense window, or at the next start", strings.Join(changed, ", "))
	}
}
