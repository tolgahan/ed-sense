package engine

import (
	"runtime"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
)

// TestSwaps: 20 switches between DSX and DS4Windows on the real app, then
// new sessions and Auto's switches, while the window and the tray read the
// state. Once EDSense quits, every backend built is closed and the
// goroutines are back to where they were. Run it with -race too.
func TestSwaps(t *testing.T) {
	base := runtime.NumGoroutine()
	r := newRig(t, rigOptions{backend: "dsx"})
	a := app.New(r.store, r.loop, r.e.Backend())
	r.e.Attach(a)
	r.e.every = 20 * time.Millisecond
	stop, ran := make(chan struct{}), make(chan struct{})
	go func() {
		r.e.Run(stop)
		close(ran)
	}()
	wake, unwatch := a.Watch()
	select {
	case <-wake: // the loop ticks
	case <-time.After(5 * time.Second):
		t.Fatal("the loop does not run")
	}

	quit := make(chan struct{})
	read := make(chan struct{})
	go func() { // the tray and the window, meanwhile
		defer close(read)
		ew, estop := r.e.Watch()
		defer estop()
		for {
			select {
			case <-quit:
				return
			case <-ew:
			case <-wake:
			case <-time.After(time.Millisecond):
			}
			_ = r.e.State()
			_, _ = a.Live()
			_ = a.Words()
			_ = a.Status()
		}
	}()

	for i := range 20 {
		choice := config.BackendDS4Windows
		if i%2 == 1 {
			choice = config.BackendDSX
		}
		st, err := r.e.Choose(choice, "tray")
		if err != nil {
			t.Fatalf("switch %d: %v", i, err)
		}
		if st.Kind != backend.Kind(choice) || a.Backend() != backend.Kind(choice) || st.Switching {
			t.Fatalf("switch %d: %+v, the app on %s", i, st, a.Backend())
		}
	}

	// new sessions on the same backend
	for _, ms := range []int{30, 40} {
		if err := r.store.Set(func(c *config.Config) { c.PollMs = ms }, "window"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.e.Apply("Apply now"); err != nil {
			t.Fatal(err)
		}
	}

	// Auto, following the app that runs
	r.sys.set(func(s *sys) { s.dsx = true })
	if st, err := r.e.Choose(config.BackendAuto, "tray"); err != nil || st.Kind != backend.KindDSX {
		t.Fatalf("auto: %+v %v", st, err)
	}
	for i := range 4 {
		want := backend.KindDS4Windows
		r.sys.set(func(s *sys) { s.dsx, s.ds4 = false, true })
		if i%2 == 1 {
			want = backend.KindDSX
			r.sys.set(func(s *sys) { s.dsx, s.ds4 = true, false })
		}
		waitFor(t, "Auto's switch", func() bool { return r.e.State().Kind == want && a.Backend() == want })
	}

	close(quit)
	<-read
	unwatch()
	close(stop)
	<-ran
	r.e.Close()
	if n := r.bld.open(); n != 0 {
		t.Errorf("%d backends left open", n)
	}
	if n := len(r.bld.built()); n != 1+20+4 {
		t.Errorf("%d backends built", n)
	}
	for end := time.Now().Add(5 * time.Second); runtime.NumGoroutine() > base; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines, %d before:\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
	}
}
