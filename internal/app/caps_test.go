package app

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
)

// lesser puts the DSX parts together as a backend that can do only c, with
// no setup work.
func lesser(c backend.Caps) func(backend.DSXParts) *backend.Backend {
	return func(parts backend.DSXParts) *backend.Backend {
		b := backend.NewDSX(parts)
		b.Caps, b.NewSetup = c, nil
		return b
	}
}

func transcript(p *player) string { return strings.Join(p.out, "\n") }

func mustNot(t *testing.T, all string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if strings.Contains(all, b) {
			t.Errorf("%q sent or logged:\n%s", b, all)
		}
	}
}

func must(t *testing.T, all string, good ...string) {
	t.Helper()
	for _, g := range good {
		if !strings.Contains(all, g) {
			t.Errorf("%q never sent or logged:\n%s", g, all)
		}
	}
}

// TestCapsXbox: with a backend that only rumbles (an Xbox pad), no frame or
// motion packet is sent, native haptics are not tried, nothing is set up,
// and the weapons rumble.
func TestCapsXbox(t *testing.T) {
	p := newPlayerOn(t, nil, lesser(backend.Caps{Rumble: true}))
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.writeStatus(elite.Status{Flags: weaponsOut, GuiFocus: 6})
	p.run(4 * time.Second)
	p.writeStatus(elite.Status{Flags: weaponsOut})
	p.pad.Hold(held(dualsense.R2, 220, 0))
	p.run(2 * time.Second)
	p.stop()
	all := transcript(p)
	mustNot(t, all, `"type":1,`, `"type":2,`, `"type":5,`, `"type":6,`, `"type":8,`, "audio maintain", "Gyro off", "setup step", "synth on")
	rumbled := slices.ContainsFunc(p.out, func(l string) bool {
		return strings.HasPrefix(l, "  pad rumble ") && l != "  pad rumble 0 0"
	})
	if !rumbled {
		t.Error("no rumble while firing")
	}
}

// TestCapsDS4Windows: a backend with everything but the gyro switch
// (DS4Windows 5 drops any packet with ToMode in it) still gets the
// triggers, lights and LEDs, but never a motion packet.
func TestCapsDS4Windows(t *testing.T) {
	c := backend.DSXCaps()
	c.MotionOff, c.KeepsOverrides = false, true
	p := newPlayerOn(t, nil, lesser(c))
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.writeStatus(elite.Status{Flags: weaponsOut, GuiFocus: 6})
	p.note("galaxy map")
	p.run(4 * time.Second)
	p.writeStatus(elite.Status{Flags: weaponsOut})
	p.config(func(c *config.Config) { c.GyroAim = false })
	p.run(3 * time.Second)
	p.stop()
	all := transcript(p)
	must(t, all, `"type":1,`, `"type":2,`, `"type":5,`, `"type":6,`, "audio maintain", `"type":7,`)
	mustNot(t, all, `"type":8,`, "Gyro off", "Gyro back")
}

// TestRumbleGate: where rumble mutes native haptics, the pad rumbles only
// while haptics_mode is "rumble", in the loop and in the demo, and a
// settings change in place applies at once. DSX's pad is not wrapped.
func TestRumbleGate(t *testing.T) {
	p := newDS4WPlayer(t, nil)
	a := p.s.App
	p.rec.Take()
	for _, c := range []struct {
		mode string
		want string
	}{
		{config.HapticsAuto, "pad rumble 0 0"},
		{config.HapticsNative, "pad rumble 0 0"},
		{config.HapticsRumble, "pad rumble 200 50"},
		{config.HapticsAuto, "pad rumble 0 0"},
	} {
		a.cfg.HapticsMode = c.mode
		a.pad.SetRumble(200, 50)
		if got := p.rec.Take(); !slices.Equal(got, []string{c.want}) {
			t.Errorf("%s: %q, want %q", c.mode, got, c.want)
		}
		a.demoOutput().Pad.SetRumble(200, 50)
		if got := p.rec.Take(); !slices.Equal(got, []string{c.want}) {
			t.Errorf("%s, the demo: %q, want %q", c.mode, got, c.want)
		}
	}
	d := newPlayer(t, nil)
	if d.s.App.pad != backend.Pad(d.pad) {
		t.Errorf("DSX's pad is wrapped: %T", d.s.App.pad)
	}
}

// TestSetupWiring: the session builds the DSX profile work with its backups
// in the data folder, telling the player through the app.
func TestSetupWiring(t *testing.T) {
	p := newPlayer(t, nil)
	if want := filepath.Join(p.dir, "dsx_profile_backups"); p.backupDir != want {
		t.Fatalf("backups go to %q, want %q", p.backupDir, want)
	}
	p.notify("hello")
	if got := p.rec.Take(); !slices.Contains(got, `notify "hello"`) {
		t.Errorf("the message did not reach the player: %q", got)
	}
}

// TestCapsOneOutput: each output cap alone lets through only its own
// instruction type.
func TestCapsOneOutput(t *testing.T) {
	types := map[string]string{"triggers": `"type":1,`, "lightbar": `"type":2,`, "mic": `"type":5,`, "leds": `"type":6,`}
	for name, c := range map[string]backend.Caps{
		"triggers": {Triggers: true}, "lightbar": {Lightbar: true},
		"mic": {Mic: true}, "leds": {PlayerLEDs: true},
	} {
		t.Run(name, func(t *testing.T) {
			p := newPlayerOn(t, nil, lesser(c))
			p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
			p.writeStatus(elite.Status{Flags: weaponsOut})
			p.run(4 * time.Second)
			p.stop()
			all := transcript(p)
			for n, typ := range types {
				if n == name {
					must(t, all, typ)
				} else {
					mustNot(t, all, typ)
				}
			}
		})
	}
}

// TestCapsNoGyro: a pad without a gyro (an Xbox pad) gets no gyro check
// after a minute of flight, so no hint about DSX's Motion passthrough.
func TestCapsNoGyro(t *testing.T) {
	c := backend.DSXCaps()
	c.Gyro = false
	p := newPlayerOn(t, nil, lesser(c))
	gyroCheck(p, 0)
	mustNot(t, transcript(p), "Motion passthrough", "Gyro:")
}

// TestCapsOwnGyro: EDSense's gyro aims only where the backend streams the
// motion and can switch its own gyro mouse off, or both would move the
// mouse. Elsewhere it never moves it, even with gyro_by "edsense".
func TestCapsOwnGyro(t *testing.T) {
	ds4 := backend.DSXCaps()
	ds4.MotionOff = false
	noGyro := backend.DSXCaps()
	noGyro.Gyro = false
	for name, c := range map[string]backend.Caps{"ds4windows": ds4, "xbox": {Rumble: true}, "no gyro": noGyro} {
		t.Run(name, func(t *testing.T) {
			p := newPlayerOn(t, ownGyro, lesser(c))
			flying(p)
			p.pad.Stream = turn(20, 0, 0, false)
			p.run(3 * time.Second)
			p.stop()
			mustNot(t, transcript(p), "  mouse ", `"type":8,`, "EDSense gyro")
		})
	}
}
