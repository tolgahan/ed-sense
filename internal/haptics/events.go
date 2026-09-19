package haptics

import (
	"slices"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

// OnStatus plays the feel of ship systems switching, from one Status.json
// to the next.
func (e *Engine) OnStatus(old, s elite.Status, now time.Time) {
	if !s.InShip() {
		return
	}
	changed := func(f elite.Flags) bool { return old.Flags&f != s.Flags&f }
	rose := func(f elite.Flags) bool { return !old.Flags.Has(f) && s.Flags.Has(f) }
	play := func(effect string) { e.playNow(effect, now) }

	if changed(elite.HardpointsDeployed) && !s.Flags.Has(elite.Supercruise) {
		play("hardpoints")
	}
	if changed(elite.LandingGearDown) {
		play("landing_gear")
	}
	if changed(elite.CargoScoopDeployed) {
		play("cargo_scoop")
	}
	if changed(elite.SilentRunning) {
		play("silent_running")
	}
	if rose(elite.FSDJump) && !s.Flags.Has(elite.Supercruise) {
		play("fsd_jump")
	}
	if len(s.Pips) == 3 && len(old.Pips) == 3 && !slices.Equal(s.Pips, old.Pips) {
		play("pips")
	}
	if !old.InShip() {
		return
	}
	if s.FireGroup != old.FireGroup {
		play("fire_group")
	}
}

// journalFeels: journal events and their one-shots.
var journalFeels = map[string]string{
	"SupercruiseEntry":   "supercruise_in",
	"SupercruiseExit":    "supercruise_out",
	"Docked":             "docked",
	"Undocked":           "undocked",
	"Touchdown":          "touchdown",
	"Liftoff":            "liftoff",
	"LaunchSRV":          "vehicle",
	"DockSRV":            "vehicle",
	"LaunchFighter":      "vehicle",
	"DockFighter":        "vehicle",
	"HeatDamage":         "heat_damage",
	"Died":               "died",
	"FSSDiscoveryScan":   "honk",
	"JetConeBoost":       "jet_cone",
	"Interdicted":        "interdicted",
	"EscapeInterdiction": "escaped",
	"CockpitBreached":    "hull_breach",
	"LaunchDrone":        "limpet",
}

// OnEvent plays the feel of a live journal event.
func (e *Engine) OnEvent(ev elite.Event, g *game.State, now time.Time) {
	play := func(effect string) { e.playNow(effect, now) }
	// the rumble motors can't make a double tap in one pulse: it is played twice
	twice := func(effect string, gap time.Duration) {
		play(effect)
		e.play(Shot{Effect: effect, At: now.Add(gap), Scale: 1})
	}
	name := ev.Name()
	if effect, ok := journalFeels[name]; ok {
		play(effect)
		return
	}
	switch name {
	case "ShieldState":
		if ev.Bool("ShieldsUp") {
			twice("shields_up", 150*time.Millisecond)
		} else {
			play("shields_down")
		}
	case "HullDamage":
		if ev.Bool("Fighter") == g.Status.Flags.Has(elite.InFighter) {
			play("hull_hit")
		}
	case "UnderAttack":
		if ev.Text("Target") == "You" {
			play("under_attack")
		}
	case "Bounty", "FactionKillBond", "CapShipBond":
		twice("kill", 160*time.Millisecond)
	case "HeatWarning":
		for i := range 3 {
			e.play(Shot{Effect: "heat_warning", At: now.Add(time.Duration(i) * 160 * time.Millisecond), Scale: 1})
		}
	case "DockingGranted":
		twice("docking_granted", 120*time.Millisecond)
	case "Scanned":
		// one sweep per scanning ship: a scan often comes as several events
		if now.Sub(e.lastScan) <= 10*time.Second {
			return
		}
		e.lastScan = now
		// left, then right
		e.play(Shot{Effect: "scanned", At: now, Side: LeftSide, Scale: 1})
		e.play(Shot{Effect: "scanned", At: now.Add(300 * time.Millisecond), Side: RightSide, Scale: 1})
	case "ShipTargeted":
		if ev.Bool("TargetLocked") && !ev.Has("ScanStage") {
			play("target_locked")
		}
	}
}
