package haptics

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/bindings"
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
	fell := func(f elite.Flags) bool { return old.Flags.Has(f) && !s.Flags.Has(f) }
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
	if changed(elite.FlightAssistOff) {
		if s.Flags.Has(elite.FlightAssistOff) {
			play("fa_off")
		} else {
			play("fa_on")
		}
	}
	if fell(elite.FSDCooldown) && !s.Flags.Has(elite.Docked) {
		play("fsd_ready")
	}
	// mass lock, but not as a side effect of leaving supercruise or a
	// station, which have their own feel
	if !s.Flags.Has(elite.Supercruise|elite.Docked) && !old.Flags.Has(elite.Supercruise|elite.Docked) {
		if rose(elite.MassLocked) {
			play("mass_lock")
		}
		if fell(elite.MassLocked) {
			play("mass_unlock")
		}
	}
	if !old.Flags2.Has(elite.GlideMode) && s.Flags2.Has(elite.GlideMode) {
		play("glide_start")
	}
	if old.Flags2.Has(elite.GlideMode) && !s.Flags2.Has(elite.GlideMode) {
		play("glide_end")
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
	"SystemsShutdown":    "systems_shutdown",
	"CollectCargo":       "cargo_collect",
	"EjectCargo":         "cargo_eject",
	"LaunchDrone":        "limpet",
}

// OnEvent plays the feel of a live journal event.
func (e *Engine) OnEvent(ev elite.Event, g *game.State, now time.Time) {
	play := func(effect string) { e.playNow(effect, now) }
	// the rumble motors can't make a double tap in one pulse: it is played twice
	twice := func(effect string, gap time.Duration) {
		play(effect)
		if !e.native {
			e.play(Shot{Effect: effect, At: now.Add(gap), Scale: 1})
		}
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
		if e.native {
			play("heat_warning")
			return
		}
		for i := range 3 {
			e.play(Shot{Effect: "heat_warning", At: now.Add(time.Duration(i) * 160 * time.Millisecond), Scale: 1})
		}
	case "ReceiveText":
		// players and the wing only: NPC lines come on the "npc" channel with $-names
		if from := ev.Text("From"); ev.Text("Channel") != "npc" && from != "" && !strings.HasPrefix(from, "$") {
			play("message")
		}
	case "DockingGranted":
		twice("docking_granted", 120*time.Millisecond)
	case "Scanned":
		// one sweep per scanning ship: a scan often comes as several events
		if now.Sub(e.lastScan) <= 10*time.Second {
			return
		}
		e.lastScan = now
		if e.native {
			play("scanned")
			return
		}
		// rumble: left, then right
		e.play(Shot{Effect: "scanned", At: now, Side: LeftSide, Scale: 1})
		e.play(Shot{Effect: "scanned", At: now.Add(300 * time.Millisecond), Side: RightSide, Scale: 1})
	case "ShipTargeted":
		if ev.Bool("TargetLocked") && !ev.Has("ScanStage") {
			play("target_locked")
		}
	}
}

// SetBindings: which actions come from the player's bindings, and which
// stick axes turn the ship.
func (e *Engine) SetBindings(b *bindings.Bindings) {
	e.boostFromBindings = b.Has(bindings.Boost)
	e.turn.sticks = defaultTurnSticks
	if b != nil && b.TurnSticks != ([4]bool{}) {
		e.turn.sticks = b.TurnSticks
	}
}

// OnActions plays the feel of bound actions the player just used.
func (e *Engine) OnActions(actions []bindings.Action, g *game.State, now time.Time) {
	s := g.Status
	if len(actions) == 0 || !s.InShip() || s.Flags.Has(elite.Docked) || s.InPanel() || g.ShutdownPhase(now) == game.SystemsDead {
		return
	}
	for _, a := range actions {
		switch a {
		case bindings.HeatSink:
			e.playNow("heat_sink", now)
			e.heat = math.Min(e.heat, 0.1) // a heat sink takes the heat away
		case bindings.Chaff:
			e.playNow("chaff", now)
		case bindings.ShieldCell:
			e.playNow("shield_cell", now)
		case bindings.Boost:
			if !s.Flags.Has(elite.Supercruise | elite.Landed) {
				e.playNow("boost", now)
			}
		}
	}
}
