package lights

import (
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

var testStart = time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)

const ship = elite.InMainShip | elite.ShieldsUp

func setup() (*config.Config, *game.State, *Renderer) {
	cfg := config.Default()
	return &cfg, game.New(), New(&cfg)
}

func TestWeaponsFrame(t *testing.T) {
	_, g, r := setup()
	g.OnStatus(elite.Status{Flags: ship | elite.HardpointsDeployed, FireGroup: 1}, testStart)
	f := r.Frame(g, testStart)
	want := dsx.Frame{
		Left:       dsx.Trigger{Mode: dsx.TriggerWeapon, Params: []int{2, 5, 4}},
		Right:      dsx.Trigger{Mode: dsx.TriggerWeapon, Params: []int{2, 5, 6}},
		Lightbar:   [3]int{0, 255, 60},
		Brightness: 200,
		PlayerLEDs: dsx.LitLEDs(2),
		Mic:        dsx.MicOff,
	}
	if f.Left.Mode != want.Left.Mode || f.Right.Mode != want.Right.Mode || f.Lightbar != want.Lightbar || f.Brightness != want.Brightness || f.PlayerLEDs != want.PlayerLEDs || f.Mic != want.Mic {
		t.Fatalf("frame %+v, want %+v", f, want)
	}
}

func TestTriggerStates(t *testing.T) {
	cfg, g, r := setup()
	now := testStart
	health := 1.0
	for _, c := range []struct {
		name   string
		status elite.Status
		want   dsx.TriggerMode
	}{
		{"hardpoints in", elite.Status{Flags: ship}, dsx.TriggerOff},
		{"hardpoints out", elite.Status{Flags: ship | elite.HardpointsDeployed}, dsx.TriggerWeapon},
		{"analysis mode", elite.Status{Flags: ship | elite.HardpointsDeployed | elite.AnalysisMode}, dsx.TriggerFeedback},
		{"galaxy map", elite.Status{Flags: ship | elite.HardpointsDeployed, GuiFocus: 6}, dsx.TriggerOff},
		{"docked", elite.Status{Flags: ship | elite.HardpointsDeployed | elite.Docked}, dsx.TriggerOff},
		{"on foot in a station", elite.Status{Flags2: elite.OnFoot | elite.OnFootSocialSpace | elite.OnFootInStation, Health: &health}, dsx.TriggerOff},
		{"on foot outside", elite.Status{Flags2: elite.OnFoot | elite.OnFootOnPlanet, Health: &health}, dsx.TriggerWeapon},
	} {
		g.OnStatus(c.status, now)
		if f := r.Frame(g, now); f.Right.Mode != c.want {
			t.Errorf("%s: R2 %v, want %v", c.name, f.Right, c.want)
		}
	}
	g.OnStatus(elite.Status{Flags: ship | elite.Docked}, now)
	if f := r.Frame(g, now); f.Brightness > cfg.Brightness/2 {
		t.Errorf("docked: dim, got %d", f.Brightness)
	}
}

func TestHyperspaceCountdownLEDs(t *testing.T) {
	_, g, r := setup()
	g.OnStatus(elite.Status{Flags: ship}, testStart)
	g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, testStart)
	if f := r.Frame(g, testStart.Add(time.Second)); f.PlayerLEDs != dsx.LitLEDs(5) {
		t.Errorf("start of the countdown: %v", f.PlayerLEDs)
	}
	if f := r.Frame(g, testStart.Add(17*time.Second)); f.PlayerLEDs != dsx.LitLEDs(1) {
		t.Errorf("end of the countdown: %v", f.PlayerLEDs)
	}
}

func TestMomentsFlashAndBuzz(t *testing.T) {
	cfg, g, r := setup()
	g.OnStatus(elite.Status{Flags: ship}, testStart)
	g.OnEvent(elite.Event{"event": "UnderAttack", "Target": "You"}, true, testStart)
	f := r.Frame(g, testStart.Add(50*time.Millisecond))
	if f.Lightbar != cfg.Color("hit") || f.Right.Mode != dsx.TriggerVibration {
		t.Fatalf("attacked: %+v", f)
	}
	g.OnEvent(elite.Event{"event": "Bounty"}, true, testStart.Add(100*time.Millisecond))
	if f := r.Frame(g, testStart.Add(150*time.Millisecond)); f.Lightbar != cfg.Color("kill") || f.Right.Mode != dsx.TriggerVibration {
		t.Fatalf("the latest flash wins, the buzz goes on: %+v", f)
	}
	if f := r.Frame(g, testStart.Add(time.Second)); f.Lightbar == cfg.Color("kill") || f.Right.Mode != dsx.TriggerOff {
		t.Fatalf("over: %+v", f)
	}
}

func TestShutdownLights(t *testing.T) {
	cfg, g, r := setup()
	g.OnStatus(elite.Status{Flags: ship | elite.HardpointsDeployed}, testStart)
	g.OnEvent(elite.Event{"event": "SystemsShutdown"}, true, testStart)
	if f := r.Frame(g, testStart.Add(time.Second)); f.Brightness != 0 || f.Right.Mode != dsx.TriggerOff {
		t.Fatalf("dead ship: %+v", f)
	}
	if f := r.Frame(g, testStart.Add(time.Minute)); f.Right.Mode != dsx.NewTrigger(cfg.TriggerFX["ship_weapons_r"].Mode, nil).Mode {
		t.Fatal("systems back after the reboot")
	}
}
