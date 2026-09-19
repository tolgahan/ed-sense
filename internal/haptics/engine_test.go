package haptics

import (
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

var testStart = time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)

const ship = elite.InMainShip | elite.ShieldsUp

type rig struct {
	cfg *config.Config
	g   *game.State
	e   *Engine
	now time.Time
}

func newRig() *rig {
	cfg := config.Default()
	return &rig{cfg: &cfg, g: game.New(), e: New(&cfg), now: testStart}
}

func (r *rig) status(flags elite.Flags, flags2 elite.Flags2) {
	s := elite.Status{Flags: flags, Flags2: flags2}
	if r.g.HaveStatus {
		r.e.OnStatus(r.g.Status, s, r.now)
	}
	r.g.OnStatus(s, r.now)
}

// run ticks n times, 25 ms apart.
func (r *rig) run(n int, pad dualsense.State) (left, right float64) {
	for range n {
		r.now = r.now.Add(25 * time.Millisecond)
		left, right = r.e.Tick(r.now, r.g, pad)
	}
	return left, right
}

// played returns how many one-shots were queued and forgets them.
func (r *rig) played() int {
	n := len(r.e.pulses)
	r.e.pulses = nil
	return n
}

var pad = dualsense.State{OK: true}

func pulled(r2, l2 bool) dualsense.State {
	p := pad
	if r2 {
		p.R2 = 255
	}
	if l2 {
		p.L2 = 255
	}
	return p
}

func TestRumbleFiring(t *testing.T) {
	r := newRig()
	r.status(ship, 0)
	if l, rr := r.e.Tick(r.now, r.g, dualsense.State{OK: true, R2: 255, Buttons: dualsense.R2}); l != 0 || rr != 0 {
		t.Fatalf("hardpoints in: silent, got %v %v", l, rr)
	}
	r.status(ship|elite.HardpointsDeployed, 0)
	if l, rr := r.e.Tick(r.now, r.g, pad); l < 0.3 || rr < 0.3 {
		t.Fatalf("hardpoints clack: %v %v", l, rr)
	}
	later := r.now.Add(time.Second)
	if l, rr := r.e.Tick(later, r.g, pulled(true, false)); l != 0 || rr < 0.15 {
		t.Fatalf("R2 on the right: %v %v", l, rr)
	}
	if l, rr := r.e.Tick(later, r.g, pulled(false, true)); rr != 0 || l < 0.15 {
		t.Fatalf("L2 on the left: %v %v", l, rr)
	}
	r.g.Status.GuiFocus = 6
	if l, rr := r.e.Tick(later, r.g, pulled(true, false)); l != 0 || rr != 0 {
		t.Fatalf("no firing in the galaxy map: %v %v", l, rr)
	}
	r.g.Status.GuiFocus = 0
	r.e.Tick(later, r.g, dualsense.State{OK: true, Pressed: dualsense.Circle, Buttons: dualsense.Circle})
	if l, rr := r.e.Tick(later.Add(100*time.Millisecond), r.g, pad); l < 0.5 || rr < 0.5 {
		t.Fatalf("Circle boosts: %v %v", l, rr)
	}
	if l, rr := r.e.Tick(later.Add(5*time.Second), r.g, pad); l != 0 || rr != 0 {
		t.Fatalf("the pulses end: %v %v", l, rr)
	}
	r.cfg.HapticsStrength = 0
	r.e.playNow("boost", later)
	if l, rr := r.e.Tick(later, r.g, pad); l != 0 || rr != 0 {
		t.Fatal("strength 0 mutes")
	}
}

func TestRumbleEvents(t *testing.T) {
	r := newRig()
	r.status(ship, 0)
	r.e.OnEvent(elite.Event{"event": "ShieldState", "ShieldsUp": false}, r.g, r.now)
	if l, rr := r.e.Tick(r.now, r.g, dualsense.State{}); l < 0.9 || rr < 0.9 {
		t.Fatalf("shields down is heavy: %v %v", l, rr)
	}
	r.e.pulses = nil
	r.e.OnEvent(elite.Event{"event": "HullDamage", "Fighter": true}, r.g, r.now)
	if l, _ := r.e.Tick(r.now, r.g, dualsense.State{}); l != 0 {
		t.Fatal("the fighter's hull damage")
	}
	r.e.OnStatus(elite.Status{Flags: elite.InMainShip, Pips: []int{4, 4, 4}}, elite.Status{Flags: elite.InMainShip, Pips: []int{6, 2, 4}}, r.now)
	if _, rr := r.e.Tick(r.now, r.g, dualsense.State{}); rr == 0 {
		t.Fatal("pips tick on the right")
	}
}

func TestRumbleAmbience(t *testing.T) {
	r := newRig()
	r.status(ship|elite.FSDCharging, 0)
	if l, rr := r.run(1, pad); l == 0 || rr == 0 {
		t.Fatalf("the FSD charging: %v %v", l, rr)
	}
	r.status(ship|elite.Overheating, 0)
	on, off := 0, 0
	for range 40 {
		if l, _ := r.run(1, pad); l > 0 {
			on++
		} else {
			off++
		}
	}
	if on == 0 || off == 0 {
		t.Fatalf("overheating pulses on and off: %d on, %d off", on, off)
	}
	r.status(ship, 0)
	if l, rr := r.run(1, dualsense.State{OK: true, Buttons: dualsense.R1}); l == 0 || rr == 0 {
		t.Fatalf("R1 thrust: %v %v", l, rr)
	}
}

func TestOnFootShots(t *testing.T) {
	r := newRig()
	r.g.OnStatus(elite.Status{Flags2: elite.OnFoot | elite.OnFootOnPlanet}, r.now)
	if l, rr := r.run(1, dualsense.State{OK: true, R2: 255, Pressed: dualsense.R2}); l != 0 || rr == 0 {
		t.Fatalf("a shot on the right: %v %v", l, rr)
	}
}

// No one-shots in danger; a scan is felt on the left, then on the right.
func TestDangerIsQuietAndScansSweep(t *testing.T) {
	r := newRig()
	r.status(ship|elite.InDanger, 0)
	quiet := func(d time.Duration) bool {
		for end := r.now.Add(d); r.now.Before(end); {
			if l, rr := r.run(1, pad); l != 0 || rr != 0 {
				return false
			}
		}
		return true
	}
	if !quiet(5 * time.Second) {
		t.Fatal("in danger: rumble")
	}
	r.status(elite.InMainShip|elite.InDanger, 0) // shields collapsed
	if !quiet(3 * time.Second) {
		t.Fatal("shields down: rumble")
	}
	for _, scan := range []string{"Cargo", "Crime", "Cabin"} { // they come together
		r.e.OnEvent(elite.Event{"event": "Scanned", "ScanType": scan}, r.g, r.now)
	}
	if n := len(r.e.pulses); n != 2 {
		t.Fatalf("scan: %d pulses", n)
	}
	if l, rr := r.e.Tick(r.now, r.g, pad); l == 0 || rr != 0 {
		t.Fatalf("left first: %v %v", l, rr)
	}
	if l, rr := r.e.Tick(r.now.Add(300*time.Millisecond), r.g, pad); l != 0 || rr == 0 {
		t.Fatalf("then right: %v %v", l, rr)
	}
}

func TestStatusFeels(t *testing.T) {
	r := newRig()
	r.status(ship, 0)
	r.played()
	for _, c := range []struct {
		flags elite.Flags
		want  int
	}{
		{ship | elite.HardpointsDeployed, 1},
		{ship | elite.Supercruise, 0}, // the hardpoints fold on entering supercruise
		{ship | elite.FSDJump, 1},
		{ship | elite.LandingGearDown, 1},
		{ship | elite.LandingGearDown | elite.SilentRunning, 1},
	} {
		r.status(c.flags, 0)
		if got := r.played(); got != c.want {
			t.Errorf("%x: %d one-shots, want %d", c.flags, got, c.want)
		}
	}
}

func TestJournalFeels(t *testing.T) {
	r := newRig()
	r.status(ship, 0)
	for _, c := range []struct {
		ev   elite.Event
		want int
	}{
		{elite.Event{"event": "Docked"}, 1},
		{elite.Event{"event": "HeatWarning"}, 3},
		{elite.Event{"event": "DockingGranted"}, 2},
		{elite.Event{"event": "ShipTargeted", "TargetLocked": true}, 1},
		{elite.Event{"event": "ShipTargeted", "TargetLocked": true, "ScanStage": 1.0}, 0},
	} {
		r.e.OnEvent(c.ev, r.g, r.now)
		if got := r.played(); got != c.want {
			t.Errorf("%v: %d one-shots, want %d", c.ev["event"], got, c.want)
		}
	}
}
