package demo

import (
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
)

const (
	ship       = elite.InMainShip | elite.ShieldsUp
	weaponsOut = ship | elite.HardpointsDeployed
)

func status(g *game.State, now time.Time, flags elite.Flags, flags2 elite.Flags2) {
	g.OnStatus(elite.Status{Flags: flags, Flags2: flags2}, now)
}

func event(g *game.State, h *haptics.Engine, now time.Time, ev elite.Event) {
	g.OnEvent(ev, true, now)
	h.OnEvent(ev, g, now)
}

func play(h *haptics.Engine, effect string, at time.Time) {
	h.Play(haptics.Shot{Effect: effect, At: at})
}

func steps() []step {
	return []step{
		{"Normal space, hull 100% (green)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
		}},
		{"Hardpoints deployed: clack, R2/L2 weapon click", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut, 0)
			play(h, "hardpoints", n)
		}},
		{"FIRE: hold R2 (right) and L2 (left)", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut, 0)
		}},
		{"THRUST: hold R1 (light rumble), press Circle to BOOST", 6 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
		}},
		{"Fire group 3 (player LEDs) and a tick", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.OnStatus(elite.Status{Flags: weaponsOut, FireGroup: 2}, n)
			play(h, "fire_group", n)
			play(h, "pips", n.Add(600*time.Millisecond))
		}},
		{"Hull 50% (amber)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Hull = 0.5
			status(g, n, weaponsOut, 0)
		}},
		{"Hull 15% (red)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Hull = 0.15
			status(g, n, weaponsOut, 0)
		}},
		{"Taking hits: thumps and red flashes", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Hull = 0.8
			status(g, n, weaponsOut, 0)
			for i := range 3 {
				at := n.Add(time.Duration(i) * 1200 * time.Millisecond)
				g.Moments = append(g.Moments, game.Moment{Kind: game.Attacked, At: at})
				play(h, "hull_hit", at)
			}
			h.OnEvent(elite.Event{"event": "UnderAttack", "Target": "You"}, g, n)
		}},
		{"Scanned by another ship: left, then right", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			event(g, h, n, elite.Event{"event": "Scanned", "ScanType": "Cargo"})
		}},
		{"Kill confirmed: double tap, green flash", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			event(g, h, n, elite.Event{"event": "Bounty"})
		}},
		{"Combat music: the lightbar breathes", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Music = "Combat_Dogfight"
			status(g, n, weaponsOut, 0)
		}},
		{"Shields down: a heavy hit, red blinking", 5 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			event(g, h, n, elite.Event{"event": "ShieldState", "ShieldsUp": false})
			status(g, n, elite.InMainShip|elite.HardpointsDeployed|elite.InDanger, 0)
		}},
		{"Overheating (over 100%): orange blinking, a pulsing rumble", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut|elite.Overheating, 0)
		}},
		{"Scanners out (analysis mode): light feedback", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut|elite.AnalysisMode, 0)
		}},
		{"FSD charging: a rising rumble", 5 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			status(g, n, ship|elite.FSDCharging, 0)
		}},
		{"Hyperspace jump: a thump, the tunnel rumbling, the LEDs counting down", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.HyperspaceStart = n.Add(-8 * time.Second)
			status(g, n, ship|elite.FSDJump, 0)
			play(h, "fsd_jump", n)
		}},
		{"Supercruise entry (blue)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Supercruise, 0)
			play(h, "supercruise_in", n)
		}},
		{"Being interdicted: magenta, a strong rumble", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Supercruise|elite.BeingInterdicted, 0)
		}},
		{"Supercruise drop", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "supercruise_out", n)
		}},
		{"Fuel scooping: amber breathing, a light rumble", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Supercruise|elite.ScoopingFuel, 0)
		}},
		{"Landing gear, low fuel (the mic LED pulses)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.LowFuel|elite.LandingGearDown, 0)
			play(h, "landing_gear", n)
		}},
		{"Silent running: the lightbar off, the mic LED on", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.SilentRunning, 0)
			play(h, "silent_running", n)
		}},
		{"Docked: the clamps thump, the lights dim", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Docked, 0)
			play(h, "docked", n)
		}},
		{"On foot, health 60%: R2 weapon, L2 aim", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			health := 0.6
			g.OnStatus(elite.Status{Flags2: elite.OnFoot | elite.OnFootOnPlanet, Health: &health}, n)
		}},
	}
}
