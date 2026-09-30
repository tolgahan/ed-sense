package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/backend/backendtest"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/haptics"
)

var update = flag.Bool("update", false, "rewrite the transcripts in testdata/golden")

// base is where the scripted clock starts. The lights blink and breathe on
// the clock, so it is fixed.
var base = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const (
	ship       = elite.InMainShip | elite.ShieldsUp
	weaponsOut = ship | elite.HardpointsDeployed
)

// TestGolden plays scripted sessions through the loop and compares all it
// asks of the backend with testdata/golden. Run with -update to record.
// The DS4Windows sessions never send a ToMode (type 8), which makes
// DS4Windows drop the whole packet.
func TestGolden(t *testing.T) {
	play := func(sc script, newP func(*testing.T, func(*config.Config)) *player) {
		t.Run(sc.name, func(t *testing.T) {
			p := newP(t, sc.cfg)
			sc.play(p)
			p.flush()
			p.sum()
			got := strings.Join(p.out, "\n") + "\n"
			backendtest.Compare(t, filepath.Join("testdata", "golden", sc.name+".txt"), got, *update)
			if p.ds4w && strings.Contains(got, `"type":8`) {
				t.Error("a ToMode was sent to DS4Windows")
			}
			if p.ds4w && !strings.Contains(sc.name, "rumble") && rumbles(p.out) {
				t.Error("rumble through DS4Windows, which mutes native haptics")
			}
		})
	}
	for _, sc := range append(scripts, gyroScripts...) {
		play(sc, newPlayer)
	}
	for _, sc := range ds4wScripts {
		play(sc, newDS4WPlayer)
	}
}

// rumbles: a line sets a motor.
func rumbles(lines []string) bool {
	return slices.ContainsFunc(lines, func(l string) bool {
		return strings.HasPrefix(l, "  pad rumble ") && l != "  pad rumble 0 0"
	})
}

type script struct {
	name string
	cfg  func(c *config.Config)
	play func(p *player)
}

// player runs one scripted session on a scripted clock.
type player struct {
	t     *testing.T
	s     *session
	dsx   *backendtest.DSX
	pad   *backendtest.Pad
	audio *backendtest.Audio
	mouse *backendtest.Mouse
	ear   backendtest.Ear // hashes the native haptics of each stretch of the script
	setup *backendtest.Setup
	watch backend.Setup // what the backend is given as its setup: setup, or a DS4Windows one around it
	ds4w  bool          // on the DS4Windows backend
	rec   *backendtest.Recorder
	dir   string
	now   time.Time

	front   bool // Elite's window is in front, for the gyro
	blocked bool // Elite runs as administrator

	out  []string
	prev []string // the last tick's lines, to fold repeats
	same int

	assemble  func(backend.DSXParts) *backend.Backend
	backupDir string       // where the setup was told to keep backups
	notify    func(string) // how it was told to reach the player
}

func newPlayer(t *testing.T, edit func(c *config.Config)) *player {
	return newPlayerOn(t, edit, backend.NewDSX)
}

// newDS4WPlayer: the session on the DS4Windows backend, with the real
// client in DS4Windows' dialect and a setup that hears about the game.
func newDS4WPlayer(t *testing.T, edit func(c *config.Config)) *player {
	return newPlayerWith(t, edit, backend.NewDS4Windows, true)
}

// newPlayerOn: the parts are put together by assemble.
func newPlayerOn(t *testing.T, edit func(c *config.Config), assemble func(backend.DSXParts) *backend.Backend) *player {
	return newPlayerWith(t, edit, assemble, false)
}

func newPlayerWith(t *testing.T, edit func(c *config.Config), assemble func(backend.DSXParts) *backend.Backend, ds4w bool) *player {
	dir, err := os.MkdirTemp("", "edsense-golden")
	if err != nil {
		t.Fatal(err)
	}
	// the journal tailer keeps its file open, which Windows will not delete
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bindingsDir := filepath.Join(dir, "bindings")
	if err := os.Mkdir(bindingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.JournalDir, cfg.BindingsDir = dir, bindingsDir
	if edit != nil {
		edit(&cfg)
	}
	cfgPath := filepath.Join(dir, "edsense.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	setMod(t, cfgPath, base.Add(-time.Hour))

	rec := &backendtest.Recorder{}
	rec.Hide(dir, "<tmp>")
	oldOut, oldFlags, oldFront := log.Writer(), log.Flags(), eliteInFront
	log.SetOutput(rec)
	log.SetFlags(0)
	eliteInFront = func() bool { return false }
	t.Cleanup(func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
		eliteInFront = oldFront
	})

	p := &player{t: t, rec: rec, dir: dir, now: base, assemble: assemble, front: true, ds4w: ds4w,
		pad: &backendtest.Pad{Rec: rec}, audio: &backendtest.Audio{Rec: rec}, mouse: &backendtest.Mouse{Rec: rec}}
	if ds4w {
		watch := &backendtest.DS4WSetup{Setup: backendtest.Setup{Rec: rec}}
		p.dsx, p.setup, p.watch = backendtest.NewDS4Windows(t), &watch.Setup, watch
	} else {
		p.dsx, p.setup = backendtest.NewDSX(t), &backendtest.Setup{Rec: rec}
		p.watch = p.setup
	}
	p.writeStatus(elite.Status{})
	p.event(`{"event":"Fileheader"}`)
	p.event(`{"event":"LoadGame","Ship":"python"}`)

	a := p.newApp(cfgPath, &cfg)
	a.SetNotify(func(msg string) { rec.Add("notify %q", msg) })
	a.OnStatus(func(st Status) {
		rec.Add("status online=%v elite=%v active=%v paused=%v demo=%v context=%q", st.Online, st.EliteRunning, st.Active, st.Paused, st.Demo, st.Context)
	})
	p.s = a.newSession()
	// the scripted clock and game; the process check would read the real
	// process list
	p.s.startedAt, p.s.running, p.s.lastProcessCheck = base, true, base.Add(1000*time.Hour)
	p.section("setup")
	return p
}

// newApp builds the App around the recording parts, put together by the
// real DSX assembly. This is the only place that changes when the App's
// construction does.
func (p *player) newApp(cfgPath string, cfg *config.Config) *App {
	a := New(cfgPath, cfg, p.assemble(backend.DSXParts{
		Output:  p.dsx,
		Pad:     p.pad,
		Reports: p.pad,
		Audio: func(render func(frames []int16)) backend.Audio {
			p.audio.Render = render
			return p.audio
		},
		Profile: func(backupDir string, notify func(string)) backend.Setup {
			p.backupDir, p.notify = backupDir, notify
			return p.watch
		},
		Close: func() {},
	}))
	a.hud = nil // the screen is not read
	a.mouse = p.mouse
	a.front = func() bool { return p.front }
	a.blocked = func() bool { return p.blocked }
	return a
}

func setMod(t *testing.T, path string, at time.Time) {
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func (p *player) writeStatus(st elite.Status) {
	b, _ := json.Marshal(map[string]any{"event": "Status", "Flags": st.Flags, "Flags2": st.Flags2, "GuiFocus": st.GuiFocus, "FireGroup": st.FireGroup, "Pips": []int{4, 4, 4}})
	if err := os.WriteFile(filepath.Join(p.dir, "Status.json"), b, 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *player) event(line string) {
	f, err := os.OpenFile(filepath.Join(p.dir, "Journal.2026-01-01T120000.01.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		p.t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// config edits the settings file; the loop picks it up within 2 s.
func (p *player) config(edit func(c *config.Config)) {
	path := p.s.cfgPath
	if err := config.Update(path, edit); err != nil {
		p.t.Fatal(err)
	}
	setMod(p.t, path, p.now)
	p.note("settings edited")
}

func (p *player) note(format string, args ...any) {
	p.flush()
	p.sum()
	p.out = append(p.out, "# "+fmt.Sprintf(format, args...))
	p.prev = nil
}

// sum records the hash of the audio since the last note or section.
func (p *player) sum() {
	if s := p.ear.Sum(); s != "" {
		p.out = append(p.out, s)
	}
}

// run ticks the loop for d, one poll interval at a time.
func (p *player) run(d time.Duration) {
	step := time.Duration(p.s.cfg.PollMs) * time.Millisecond
	for end := p.now.Add(d); p.now.Before(end); p.now = p.now.Add(step) {
		p.pad.Emit(p.now, step)
		p.s.tick(p.now)
		p.listenSynth()
		p.mouse.Flush()
		lines := p.rec.Take()
		if p.prev != nil && slices.Equal(lines, p.prev) {
			p.same++
			continue
		}
		p.flush()
		p.out = append(p.out, fmt.Sprintf("@ %.3f", p.now.Sub(base).Seconds()))
		p.out = append(p.out, indent(lines)...)
		p.prev = lines
	}
}

// listenSynth records whether each actuator sounds during one tick, and
// hashes what it rendered.
func (p *player) listenSynth() {
	p.rec.Add("%s", p.ear.Listen(p.audio.Render, p.s.cfg.PollMs*48))
}

// TestDemoWiring: the demo plays through the loop's own parts, and the synth
// it drives is the one the backend's audio renders.
func TestDemoWiring(t *testing.T) {
	p := newPlayer(t, nil)
	a := p.s.App
	o := a.demoOutput()
	if o.Out != a.out || o.Pad != a.pad || o.Synth != a.synth || o.Audio != a.audio {
		t.Fatal("the demo plays through other parts than the loop's")
	}
	o.Synth.Play([]haptics.Voice{{Wave: haptics.Sine, F0: 170, Amp: 1, L: 1, R: 1, Hold: 0.1}}, 1)
	if got := backendtest.Listen(p.audio.Render, 480); got != "synth on on" {
		t.Fatalf("the demo's synth is not the one the audio plays: %s", got)
	}
}

// section records what happened outside the ticks.
func (p *player) section(label string) {
	p.flush()
	p.sum()
	p.out = append(p.out, label)
	p.out = append(p.out, indent(p.rec.Take())...)
	p.prev = nil
}

func (p *player) flush() {
	if p.same > 0 {
		p.out = append(p.out, fmt.Sprintf("  (same for %d more ticks)", p.same))
	}
	p.same = 0
}

// stop ends the session as Run does when the player quits.
func (p *player) stop() {
	p.s.stop()
	p.s.closeParts()
	p.section("stop")
}

func indent(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + l
	}
	return out
}

func held(b dualsense.Button, r2, l2 uint8) dualsense.State {
	return dualsense.State{Buttons: b, R2: r2, L2: l2}
}

var scripts = []script{
	{"dsx-late", nil, func(p *player) {
		p.writeStatus(elite.Status{Flags: ship})
		p.run(6 * time.Second)
		p.dsx.Answering = true
		p.note("DSX answers")
		p.run(4 * time.Second)
		p.stop()
	}},
	{"combat-native", nil, func(p *player) { combat(p) }},
	{"combat-rumble", func(c *config.Config) { c.HapticsMode = config.HapticsRumble }, func(p *player) { combat(p) }},
	{"audio-flips", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(time.Second)
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(time.Second)
		p.audio.Up = false
		p.note("native haptics lost")
		p.run(time.Second)
		p.audio.Up = true
		p.note("native haptics back")
		p.run(time.Second)
		p.stop()
	}},
	{"menus", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.note("galaxy map")
		p.run(7 * time.Second)
		p.writeStatus(elite.Status{Flags: ship})
		p.note("galaxy map closed")
		p.run(time.Second)
		p.event(`{"event":"Music","MusicTrack":"MainMenu"}`)
		p.note("main menu")
		p.run(4 * time.Second)
		p.event(`{"event":"LoadGame","Ship":"python"}`)
		p.note("back in the game")
		p.run(time.Second)
		p.s.SetPaused(true)
		p.note("paused")
		p.run(2 * time.Second)
		p.s.SetPaused(false)
		p.note("resumed")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-aim-off", nil, func(p *player) {
		p.dsx.Answering = true
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.config(func(c *config.Config) { c.GyroAim = false })
		p.run(5 * time.Second)
		p.config(func(c *config.Config) { c.GyroAim = true })
		p.run(3 * time.Second)
		p.stop()
	}},
	{"controllers", nil, func(p *player) {
		p.dsx.Answering = true
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.run(time.Second)
		for _, slots := range [][]int{{0, 1}, {1}, {}, {0}} {
			p.dsx.Slots = slots
			p.note("controllers %v", slots)
			p.run(time.Second)
		}
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.stop()
	}},
	{"outputs", nil, func(p *player) {
		p.dsx.Answering = true
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.config(func(c *config.Config) { c.Lightbar, c.MicLED = false, false })
		p.run(4 * time.Second)
		p.config(func(c *config.Config) { c.Triggers, c.PlayerLEDs = false, false })
		p.run(4 * time.Second)
		p.stop()
	}},
	{"haptics-off", func(c *config.Config) { c.Haptics = false }, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(2 * time.Second)
		p.stop()
	}},
	{"dsx-drops", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.run(2 * time.Second)
		p.dsx.Answering = false
		p.note("DSX stops answering")
		p.run(4 * time.Second)
		p.dsx.Answering = true
		p.note("DSX answers again")
		p.run(2 * time.Second)
		p.stop()
	}},
	// the hint about DSX's Motion passthrough is for DSX's gyro
	{"gyro-check-none", dsxGyro, func(p *player) { gyroCheck(p, 0) }},
	{"gyro-check-ok", dsxGyro, func(p *player) { gyroCheck(p, 3280) }},
	{"demo-requested", func(c *config.Config) { c.Lightbar, c.MicLED = false, false }, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(time.Second)
		// Quit after the demo's first frame: its second read of the pad
		quit, reads := make(chan struct{}), 0
		p.pad.OnRead = func() {
			if reads++; reads == 2 {
				close(quit)
			}
		}
		p.s.playDemo(quit)
		p.pad.OnRead = nil
		p.section("# the tray's Play demo, then Quit")
		p.run(time.Second)
		p.stop()
	}},
	{"demo-cli", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		quit := make(chan struct{})
		close(quit)
		p.s.App.PlayDemo(quit)
		p.section("# -demo, then Ctrl+C at once")
	}},
	{"elite-closes", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(time.Second)
		p.event(`{"event":"Shutdown"}`)
		p.note("Elite writes Shutdown")
		p.run(time.Second)
		p.s.running = false
		p.note("Elite closed")
		p.run(time.Second)
		p.s.running = true
		p.event(`{"event":"LoadGame","Ship":"python"}`)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.note("Elite back, in the galaxy map")
		p.run(time.Second)
		p.stop()
	}},
}

func combat(p *player) {
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.writeStatus(elite.Status{Flags: weaponsOut})
	p.run(500 * time.Millisecond)
	p.pad.Hold(held(dualsense.R2, 220, 0))
	p.note("R2 held")
	p.run(time.Second)
	p.pad.Tap(dualsense.L2)
	p.note("L2 tapped")
	p.run(500 * time.Millisecond)
	p.pad.Hold(held(dualsense.R2|dualsense.R1, 220, 0))
	p.note("R1 held")
	p.run(time.Second)
	p.pad.Tap(dualsense.Circle)
	p.note("Circle pressed")
	p.run(time.Second)
	p.pad.Hold(dualsense.State{})
	p.note("all released")
	p.run(time.Second)
	p.stop()
}

func gyroCheck(p *player, raw int16) {
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.writeStatus(elite.Status{Flags: ship})
	p.pad.Hold(dualsense.State{Gyro: [3]int16{0, raw, 0}})
	p.run(62 * time.Second)
	p.stop()
}
