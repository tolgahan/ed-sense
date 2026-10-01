package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/backend/backendtest"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// Restarts: the loop ends a session and starts the next one, on another
// backend (a switch) or on the same one (Apply now). The controller is
// handed back first; a switch then closes the old backend's parts and
// keeps the game as the loop knows it.
var restartScripts = []script{
	// the watcher's switch waits until EDSense no longer drives the
	// controller
	{"restart-dsx-ds4w-idle", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.note("galaxy map")
		p.run(time.Second)
		p.switchTo("# the watcher switches to DS4Windows", true, true)
		p.event(`{"event":"Music","MusicTrack":"MainMenu"}`)
		p.note("main menu")
		p.run(time.Second)
		p.switchTo("# the watcher switches to DS4Windows", true, true)
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.run(time.Second)
		p.event(`{"event":"LoadGame","Ship":"python"}`)
		p.writeStatus(elite.Status{Flags: ship})
		p.note("back in the game")
		p.run(time.Second)
		p.stop()
	}},
	{"restart-dsx-ds4w-active", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(2500 * time.Millisecond) // the gyro's drift learned
		p.switchTo("# Apply now: DS4Windows, R2 held", true, false)
		p.pad.Open, p.audio.Up = true, true
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(500 * time.Millisecond)
		p.dsx.Answering = true
		p.note("DS4Windows answers")
		p.run(time.Second)
		p.stop()
	}},
	{"restart-ds4w-dsx-active", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(time.Second)
		p.switchTo("# Apply now: DSX, R2 held", false, false)
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(time.Second)
		p.stop()
	}},
	// poll_ms: the same backend, the pad and its audio stay open, and the
	// game is kept
	{"restart-session", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(time.Second)
		p.s.cfg.PollMs = 40
		p.note("poll_ms 40 in the loop's settings")
		p.renew("# Apply now: a new session on DSX, keeping the game", true)
		p.run(time.Second)
		p.stop()
	}},
	// journal_dir: a whole new session that reads the other folder
	{"restart-dirs", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.jdir = p.tempDir("edsense-journal", "<journal>")
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.event(`{"event":"Fileheader"}`)
		p.event(`{"event":"LoadGame","Ship":"python"}`)
		p.s.cfg.JournalDir = p.jdir
		p.note("journal_dir set to another folder in the loop's settings")
		p.renew("# Apply now: a new session on DSX", false)
		p.run(time.Second)
		p.stop()
	}},
}

// span: from line from of the transcript on, the backend is DS4Windows'
// or not.
type span struct {
	from int
	ds4w bool
}

// TestGoldenRestart plays the restart scripts as TestGolden plays the
// others. A script whose name starts with restart-ds4w starts on
// DS4Windows. DS4Windows never gets a ToMode or rumble, before or after a
// switch.
func TestGoldenRestart(t *testing.T) {
	for _, sc := range restartScripts {
		t.Run(sc.name, func(t *testing.T) {
			newP := newPlayer
			if strings.HasPrefix(sc.name, "restart-ds4w") {
				newP = newDS4WPlayer
			}
			p := newP(t, sc.cfg)
			spans := []span{{0, p.ds4w}}
			p.switched = func() { spans = append(spans, span{len(p.out), p.ds4w}) }
			sc.play(p)
			p.flush()
			p.sum()
			got := strings.Join(p.out, "\n") + "\n"
			backendtest.Compare(t, filepath.Join("testdata", "golden", sc.name+".txt"), got, *update)
			for i, sp := range spans {
				end := len(p.out)
				if i+1 < len(spans) {
					end = spans[i+1].from
				}
				part := p.out[sp.from:end]
				if sp.ds4w && slices.ContainsFunc(part, func(l string) bool { return strings.Contains(l, `"type":8`) }) {
					t.Error("a ToMode was sent to DS4Windows")
				}
				if sp.ds4w && rumbles(part) {
					t.Error("rumble through DS4Windows, which mutes native haptics")
				}
			}
		})
	}
}

// tempDir is another folder for the script, shown as as in the
// transcript.
func (p *player) tempDir(prefix, as string) string {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		p.t.Fatal(err)
	}
	p.t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p.rec.Hide(dir, as)
	return dir
}

// switchTo restarts the loop on a new backend, DS4Windows' when ds4w, as
// the engine's switch does: the game is kept, and a backend the loop
// refused is closed again. idleOnly is the watcher's.
func (p *player) switchTo(label string, ds4w, idleOnly bool) {
	r := newRig(p.t, p.rec, ds4w)
	assemble := backend.NewDSX
	if ds4w {
		assemble = backend.NewDS4Windows
	}
	b := p.build(r, assemble)
	if err := p.restart(b, idleOnly, true); err != nil {
		b.Discard()
		p.section(label + ": " + err.Error())
		return
	}
	p.rig, p.assemble = r, assemble
	p.section(label)
	if p.switched != nil {
		p.switched()
	}
}

// renew restarts the loop on the same backend, as Apply now does for the
// session's keys.
func (p *player) renew(label string, adopt bool) {
	if err := p.restart(nil, false, adopt); err != nil {
		p.t.Fatal(err)
	}
	p.section(label)
}

// restart runs the loop's restart handler for b (nil: the same backend).
func (p *player) restart(b *backend.Backend, idleOnly, adopt bool) error {
	next, err := p.s.restart(restartReq{b: b, idleOnly: idleOnly, adopt: adopt}, p.now)
	if err != nil {
		if next != p.s {
			p.t.Fatal("a refused restart changed the session")
		}
		return err
	}
	p.s = next
	if !adopt {
		// the scripted clock and game, as newPlayerWith sets them
		p.s.startedAt, p.s.running, p.s.lastProcessCheck = p.now, true, p.now.Add(1000*time.Hour)
	}
	return nil
}

// TestRestartBusy: a calibration refuses any restart, and EDSense driving
// the controller refuses the watcher's (idleOnly). A refusal sends,
// closes and attaches nothing.
func TestRestartBusy(t *testing.T) {
	p := newPlayer(t, nil)
	flying(p)
	p.run(time.Second)
	a := p.s.App
	if !a.Busy() {
		t.Error("not busy while active and answering")
	}
	r := newRig(t, p.rec, true)
	b := p.build(r, backend.NewDS4Windows)
	p.rec.Take()
	refused := func(idleOnly bool, why string) {
		t.Helper()
		if _, err := p.s.restart(restartReq{b: b, idleOnly: idleOnly, adopt: true}, p.now); !errors.Is(err, ErrBusy) {
			t.Errorf("%s: %v, want busy", why, err)
		}
		if got := p.rec.Take(); len(got) > 0 {
			t.Errorf("%s: a refused restart did %q", why, got)
		}
		if a.b == b || a.Backend() != backend.KindDSX {
			t.Errorf("%s: the backend was attached", why)
		}
	}
	refused(true, "active")

	p.s.CalibrateGyro()
	p.run(100 * time.Millisecond)
	if !p.s.gyroStatus().Calibrating || !a.calibrating.Load() {
		t.Fatal("not calibrating")
	}
	p.rec.Take()
	refused(true, "calibrating")
	refused(false, "calibrating, Apply now")
	p.run(3 * time.Second)

	// idle: the main menu
	p.event(`{"event":"Music","MusicTrack":"MainMenu"}`)
	p.run(time.Second)
	if a.Busy() {
		t.Errorf("busy while idle: %+v", a.Status())
	}
	if err := p.restart(b, true, true); err != nil {
		t.Fatalf("idle: %v", err)
	}
	if a.b != b || a.Backend() != backend.KindDS4Windows {
		t.Error("the backend was not attached")
	}
	p.rig, p.assemble = r, backend.NewDS4Windows

	// active again: Apply now does not wait
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.event(`{"event":"LoadGame","Ship":"python"}`)
	p.run(time.Second)
	if !p.s.active || !p.s.online {
		t.Fatal("not active on DS4Windows")
	}
	if err := p.restart(nil, false, true); err != nil {
		t.Errorf("Apply now while active: %v", err)
	}
}

// TestRestartOffline: in flight, with the backend no longer answering,
// EDSense drives nothing, so Auto's switch (idleOnly) is taken.
func TestRestartOffline(t *testing.T) {
	p := newPlayer(t, nil)
	flying(p)
	p.run(time.Second)
	a := p.s.App
	if !a.Busy() {
		t.Fatal("not busy while active and answering")
	}
	p.dsx.Answering = false
	p.run(time.Second)
	if !p.s.active || p.s.online || a.Busy() {
		t.Fatalf("active %v, online %v, busy %v", p.s.active, p.s.online, a.Busy())
	}
	b := p.build(newRig(t, p.rec, true), backend.NewDS4Windows)
	if err := p.restart(b, true, true); err != nil {
		t.Fatalf("offline: %v", err)
	}
	if a.b != b || a.Backend() != backend.KindDS4Windows {
		t.Error("the backend was not attached")
	}
}

// TestRestartTakesWrite: a change the Store wrote just before a restart,
// whose kick the loop has not taken yet, is in the next session's
// settings.
func TestRestartTakesWrite(t *testing.T) {
	p := newPlayer(t, nil)
	p.run(time.Second)
	other := t.TempDir()
	if err := p.s.store.Set(func(c *config.Config) { c.PollMs, c.JournalDir = 40, other }, "window"); err != nil {
		t.Fatal(err)
	}
	if p.s.cfg.PollMs == 40 {
		t.Fatal("the loop took the kick already")
	}
	p.rec.Take()
	if err := p.restart(nil, false, false); err != nil {
		t.Fatal(err)
	}
	if p.s.cfg.PollMs != 40 || p.s.cfg.JournalDir != other {
		t.Errorf("poll_ms %d, journal_dir %q", p.s.cfg.PollMs, p.s.cfg.JournalDir)
	}
	if !slices.Contains(p.rec.Take(), "log Reading "+other) {
		t.Error("the next session does not read the new folder")
	}
}

// TestRestartTakesRecheck: a write whose kick a read under way used up
// (another write held the Store) is in the next session's settings.
func TestRestartTakesRecheck(t *testing.T) {
	p := newPlayer(t, nil)
	p.run(time.Second)
	if err := p.s.store.Set(func(c *config.Config) { c.PollMs = 40 }, "window"); err != nil {
		t.Fatal(err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error)
	go func() {
		done <- p.s.store.Set(func(*config.Config) {
			close(entered)
			<-release
		}, "tray")
	}()
	<-entered
	<-p.s.store.Kick()
	p.s.checkConfig(p.now, true) // the Store is busy: read again later
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p.s.cfg.PollMs == 40 {
		t.Fatal("the read while the Store was busy took the write")
	}
	if err := p.restart(nil, false, true); err != nil {
		t.Fatal(err)
	}
	if p.s.cfg.PollMs != 40 || p.s.pollMs != 40 {
		t.Errorf("poll_ms %d, the session ticks every %d ms", p.s.cfg.PollMs, p.s.pollMs)
	}
	if p.count("Settings reloaded") != 0 {
		t.Error("the Store's write said Settings reloaded")
	}
}

// TestRestartKeys: the next session is built from the keys given (the
// engine's applied settings), whatever the loop's settings hold, so a
// poll_ms or journal_dir that waits in the file keeps waiting; without
// keys it is built from the loop's settings.
func TestRestartKeys(t *testing.T) {
	p := newPlayer(t, nil)
	p.run(time.Second)
	applied := KeysOf(p.s.cfg)
	other := t.TempDir()
	p.s.cfg.PollMs, p.s.cfg.JournalDir = 50, other // read from the file, not applied

	// Auto's switch keeps the game and the applied poll_ms
	r := newRig(t, p.rec, true)
	b := p.build(r, backend.NewDS4Windows)
	next, err := p.s.restart(restartReq{b: b, idleOnly: true, adopt: true, keys: &applied}, p.now)
	if err != nil {
		t.Fatal(err)
	}
	p.s, p.rig, p.assemble = next, r, backend.NewDS4Windows
	if p.s.pollMs != applied.PollMs {
		t.Errorf("Auto's switch: the session ticks every %d ms, want %d", p.s.pollMs, applied.PollMs)
	}

	// Apply now: a new session from what it applies
	p.rec.Take()
	keys := applied
	keys.JournalDir, keys.PollMs = other, 40
	if next, err = p.s.restart(restartReq{keys: &keys}, p.now); err != nil {
		t.Fatal(err)
	}
	p.s = next
	if p.s.pollMs != 40 || !slices.Contains(p.rec.Take(), "log Reading "+other) {
		t.Errorf("Apply now: the session ticks every %d ms, or does not read %s", p.s.pollMs, other)
	}

	// no keys: the loop's settings
	if next, err = p.s.restart(restartReq{adopt: true}, p.now); err != nil {
		t.Fatal(err)
	}
	if next.pollMs != 50 {
		t.Errorf("without keys: the session ticks every %d ms", next.pollMs)
	}
}

// TestLoopTicksAtSessionPoll: the loop ticks at the session's poll_ms, not
// at a poll_ms that waits in the loop's settings.
func TestLoopTicksAtSessionPoll(t *testing.T) {
	p := newPlayer(t, nil)
	a := p.s.App
	p.s.pollMs, p.s.cfg.PollMs = 20, 60000
	wake, unwatch := a.Watch()
	defer unwatch()
	stop, ran := make(chan struct{}), make(chan struct{})
	go func() {
		p.s.loop(stop)
		close(ran)
	}()
	defer func() {
		close(stop)
		<-ran
	}()
	end := time.After(5 * time.Second)
	for n := 0; n < 5; n++ {
		select {
		case <-wake:
			a.WakeStatus() // the next tick wakes the watcher again
		case <-end:
			t.Fatalf("%d ticks in 5 s", n)
		}
	}
}

// closingSetup is a setup with work of its own to end, as DS4Windows'
// has.
type closingSetup struct {
	backend.Setup
	closed *atomic.Int32
}

func (s closingSetup) Close() { s.closed.Add(1) }

// TestSessionClosesSetup: a session ends its setup's work when a restart
// ends it (Apply now, a switch, a restart met by the quit) and when the
// loop stops, so no DS4Windows worker is left behind.
func TestSessionClosesSetup(t *testing.T) {
	p := newDS4WPlayer(t, nil)
	var closed atomic.Int32
	wrap := func(r *rig) { r.watch = closingSetup{r.watch, &closed} }
	want := func(n int32, what string) {
		t.Helper()
		if got := closed.Load(); got != n {
			t.Errorf("%s: closed %d times, want %d", what, got, n)
		}
	}
	wrap(p.rig)
	p.s = p.s.App.newSession()
	p.renew("# Apply now, keeping the game", true)
	want(1, "Apply now")
	p.renew("# Apply now, a new session", false)
	want(2, "a new session")
	r := newRig(t, p.rec, true)
	wrap(r)
	if err := p.restart(p.build(r, backend.NewDS4Windows), false, true); err != nil {
		t.Fatal(err)
	}
	p.rig = r
	want(3, "a switch")

	stop := make(chan struct{})
	close(stop)
	if next := p.s.takeRestart(restartReq{adopt: true, done: make(chan error, 1)}, stop); next != nil {
		t.Fatal("the loop goes on after the quit")
	}
	want(4, "a restart met by the quit")
	p.s = p.s.App.newSession()
	if next := p.s.loop(stop); next != nil {
		t.Fatal("the loop goes on after stop")
	}
	want(5, "the loop's stop")
}

// TestRestartBias: the drift learned goes to the old backend's file
// before the switch, and the new backend's gyro starts from its own.
func TestRestartBias(t *testing.T) {
	p := newPlayer(t, nil)
	flying(p)
	p.run(2500 * time.Millisecond)
	if !p.s.gyroStatus().Calibrated {
		t.Fatal("no drift learned")
	}
	p.switchTo("# switch to DS4Windows", true, false)
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(p.dir, name))
		return err == nil
	}
	if !exists(gyro.BiasFile) || exists(backend.DS4WindowsBiasFile) {
		t.Errorf("after the switch: %s %v, %s %v", gyro.BiasFile, exists(gyro.BiasFile), backend.DS4WindowsBiasFile, exists(backend.DS4WindowsBiasFile))
	}
	if p.s.gyroStatus().Calibrated {
		t.Error("DS4Windows' gyro starts from DSX's drift")
	}
	p.switchTo("# back to DSX", false, false)
	if !p.s.gyroStatus().Calibrated {
		t.Error("DSX's gyro did not load its drift")
	}
}

// TestCalibrateAcrossRestart: a calibration asked for just before a
// switch onto a backend without a gyro is answered, never started.
func TestCalibrateAcrossRestart(t *testing.T) {
	p := newPlayer(t, nil)
	flying(p)
	p.run(time.Second)
	p.s.CalibrateGyro() // the loop takes it at its next tick, on the next backend
	noGyro := backend.DSXCaps()
	noGyro.Gyro = false
	r := newRig(t, p.rec, false)
	if err := p.restart(p.build(r, lesser(noGyro)), false, true); err != nil {
		t.Fatal(err)
	}
	p.rig = r
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.rec.Take()
	p.run(100 * time.Millisecond)
	want := `notify "This controller connection passes no gyro."`
	if n := p.count(want); n != 1 {
		t.Errorf("told %d times:\n%s", n, strings.Join(p.out, "\n"))
	}
	p.s.CalibrateGyro()
	if got := p.rec.Take(); !slices.Equal(got, []string{want}) {
		t.Errorf("asked after the switch: %q", got)
	}
}

// TestRestartDemo: the watcher's restart is refused while a demo plays;
// Apply now cuts the demo short and restarts.
func TestRestartDemo(t *testing.T) {
	p := newPlayer(t, nil)
	p.dsx.Answering, p.audio.Up = true, true
	a := p.s.App
	stop, ran := make(chan struct{}), make(chan struct{})
	go func() {
		a.Run(stop)
		close(ran)
	}()
	defer func() {
		close(stop)
		<-ran
	}()
	a.RequestDemo()
	waitFor(t, "the demo", func() bool { return a.demoPlaying.Load() != nil })
	if !a.Busy() {
		t.Error("not busy while the demo plays")
	}
	b := p.build(newRig(t, p.rec, true), backend.NewDS4Windows)
	if err := a.Restart(b, true, true, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("the watcher's restart during the demo: %v", err)
	}
	if a.demoPlaying.Load() == nil {
		t.Error("the watcher's restart cut the demo")
	}
	if err := a.Restart(b, false, true, nil); err != nil {
		t.Fatalf("Apply now during the demo: %v", err)
	}
	if a.Backend() != backend.KindDS4Windows || a.demoPlaying.Load() != nil {
		t.Errorf("after Apply now: %s, demo %v", a.Backend(), a.demoPlaying.Load() != nil)
	}
}

// TestRestartQueuedDemo: a restart that waits goes before a demo asked
// for, whichever the loop's select takes first, and the demo plays after
// it, in the next session.
func TestRestartQueuedDemo(t *testing.T) {
	p := newPlayer(t, nil)
	a := p.s.App
	a.restarts = make(chan restartReq, 1) // the restart waits before the loop looks
	stop, quit := make(chan struct{}), make(chan struct{})
	var played atomic.Bool
	go func() { // a demo that plays is cut short at once
		for {
			select {
			case <-quit:
				return
			case <-time.After(time.Millisecond):
				if a.demoPlaying.Load() != nil {
					played.Store(true)
					a.StopDemo()
				}
			}
		}
	}()
	defer close(quit)
	for i := range 20 {
		a.RequestDemo()
		r := restartReq{adopt: true, done: make(chan error, 1)}
		a.restarts <- r
		next := p.s.loop(stop)
		if err := <-r.done; err != nil || next == nil || next == p.s {
			t.Fatalf("round %d: %v", i, err)
		}
		if played.Load() || len(a.demoRequests) != 1 {
			t.Fatalf("round %d: the demo played first, or was dropped", i)
		}
		p.s = next
	}
}

// TestRestartStopped: before Run and after it a restart is refused; one
// the loop takes with stop closed is refused too, and the controller is
// handed back. The caller then closes the backend it built.
func TestRestartStopped(t *testing.T) {
	p := newPlayer(t, nil)
	a := p.s.App
	b := p.build(newRig(t, p.rec, true), backend.NewDS4Windows)
	p.rec.Take()
	if err := a.Restart(b, false, true, nil); !errors.Is(err, ErrStopped) {
		t.Errorf("before Run: %v", err)
	}
	select {
	case <-a.Done():
		t.Error("Done is closed before Run")
	default:
	}

	stop := make(chan struct{})
	close(stop)
	r := restartReq{b: b, adopt: true, done: make(chan error, 1)}
	if next := p.s.takeRestart(r, stop); next != nil {
		t.Error("the loop goes on after stop")
	}
	if err := <-r.done; !errors.Is(err, ErrStopped) {
		t.Errorf("with stop closed: %v", err)
	}
	got := p.rec.Take()
	if !slices.Contains(got, "log Stopped") || slices.Contains(got, "backend close") || slices.Contains(got, "pad close") {
		t.Errorf("with stop closed: %q", got)
	}
	if a.b == b {
		t.Error("attached with stop closed")
	}

	done := make(chan struct{})
	go func() {
		a.Run(stop)
		close(done)
	}()
	<-done
	select {
	case <-a.Done():
	default:
		t.Error("Done is open after Run")
	}
	if err := a.Restart(b, false, true, nil); !errors.Is(err, ErrStopped) {
		t.Errorf("after Run: %v", err)
	}
	p.rec.Take()
	b.Discard()
	if got := p.rec.Take(); !slices.Equal(got, []string{"pad close", "backend close"}) {
		t.Errorf("the caller's close: %q", got)
	}
}

// TestAdoptFresh: an adopted session keeps the game side, and every other
// field starts over, so the new backend gets its hand-back and a full
// frame. A field added to session starts over unless it is listed here.
func TestAdoptFresh(t *testing.T) {
	kept := map[string]bool{"App": true, "game": true, "haptics": true, "lights": true, "status": true,
		"journal": true, "bindings": true, "detector": true, "running": true, "context": true, "target": true,
		"lastHUDHit": true, "lastConfigCheck": true, "lastProcessCheck": true}
	p := newPlayer(t, nil)
	flying(p)
	p.run(time.Second)
	old := p.s
	ov := reflect.ValueOf(old).Elem()
	field := func(v reflect.Value, i int) reflect.Value {
		f := v.Field(i)
		return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
	}
	// every field set, as a long session leaves them
	for i := range ov.NumField() {
		name := ov.Type().Field(i).Name
		if name == "App" || name == "profile" {
			continue
		}
		f := field(ov, i)
		if !kept[name] || f.IsZero() {
			fill(f)
		}
		if f.IsZero() {
			t.Fatalf("could not set %s", name)
		}
	}
	old.hold, old.saidUse = gyro.HoldMenu, backend.GyroMouse

	now := p.now.Add(time.Minute)
	n := old.adopt(now, 40)
	nv := reflect.ValueOf(n).Elem()
	for i := range nv.NumField() {
		name := nv.Type().Field(i).Name
		f := field(nv, i)
		switch name {
		case "profile":
			if n.profile != p.watch {
				t.Errorf("profile %v, want a new one from the backend", n.profile)
			}
		case "pollMs":
			if n.pollMs != 40 {
				t.Errorf("pollMs %d, want the one given", n.pollMs)
			}
		case "startedAt":
			if !n.startedAt.Equal(now) {
				t.Errorf("startedAt %v, want %v", n.startedAt, now)
			}
		case "hold":
			if n.hold != gyro.HoldStart {
				t.Errorf("hold %v", n.hold)
			}
		case "saidUse":
			if n.saidUse != -1 {
				t.Errorf("saidUse %v", n.saidUse)
			}
		default:
			if kept[name] {
				if !reflect.DeepEqual(f.Interface(), field(ov, i).Interface()) {
					t.Errorf("%s not kept", name)
				}
			} else if !f.IsZero() {
				t.Errorf("%s carried over: %v", name, f.Interface())
			}
		}
	}
}

// fill sets v to a value other than its zero value.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.String:
		v.SetString("x")
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
	case reflect.Array:
		fill(v.Index(0))
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(base))
			return
		}
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				fill(v.Field(i))
				return
			}
		}
	}
}

// reportingSetup is a setup that publishes what it found, as DS4Windows'
// does.
type reportingSetup struct {
	backend.Setup
	report *ds4w.Report
}

func (s reportingSetup) Report() *ds4w.Report { return s.report }

// TestSetupReport: the window reads what the session's setup found; a
// setup that reports nothing, after a switch, gives nil.
func TestSetupReport(t *testing.T) {
	report := &ds4w.Report{Profile: "Elite"}
	p := newDS4WPlayer(t, nil)
	p.watch = reportingSetup{p.watch, report}
	p.s = p.s.App.newSession()
	a := p.s.App
	if got := a.SetupReport(); got != report {
		t.Errorf("SetupReport %v", got)
	}
	p.switchTo("# switch to DSX", false, false)
	if got := a.SetupReport(); got != nil {
		t.Errorf("after the switch to DSX: %v", got)
	}
}

// TestRestartRace: restarts, and what the tray and the window read, from
// other goroutines while Run ticks. Run it with -race.
func TestRestartRace(t *testing.T) {
	p := newPlayer(t, nil)
	p.dsx.Answering = true
	a := p.s.App
	stop, ran := make(chan struct{}), make(chan struct{})
	go func() {
		a.Run(stop)
		close(ran)
	}()
	wake, stopWatch := a.Watch()
	quit := make(chan struct{})
	var wg sync.WaitGroup
	for _, read := range []func(){
		func() { _ = a.Status() },
		func() { _ = a.Words().Name },
		func() { _, _ = a.Live() },
		func() { _ = a.Backend() },
		func() { a.CalibrateGyro() },
		func() { _ = a.SetupReport() },
		func() { _ = a.Busy() },
		func() { a.WakeStatus() },
		func() { a.RequestDSXProfileReset() },
		func() {
			select {
			case <-wake:
			case <-time.After(time.Millisecond):
			}
		},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-quit:
					return
				default:
					read()
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}
	kinds := map[backend.Kind]bool{}
	for i := range 12 {
		var b *backend.Backend
		switch i % 3 {
		case 0:
			b = p.build(newRig(t, p.rec, true), backend.NewDS4Windows)
		case 1:
			b = p.build(newRig(t, p.rec, false), backend.NewDSX)
		}
		if err := a.Restart(b, false, i%2 == 0, nil); err != nil {
			t.Fatalf("restart %d: %v", i, err)
		}
		kinds[a.Backend()] = true
		time.Sleep(20 * time.Millisecond)
	}
	close(quit)
	wg.Wait()
	stopWatch()
	close(stop)
	<-ran
	if err := a.Restart(nil, false, true, nil); !errors.Is(err, ErrStopped) {
		t.Errorf("after Run: %v", err)
	}
	if !kinds[backend.KindDSX] || !kinds[backend.KindDS4Windows] {
		t.Errorf("backends used: %v", kinds)
	}
}

// waitFor waits up to 5 s for ok.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("no %s", what)
		}
	}
}
