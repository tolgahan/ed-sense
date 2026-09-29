package demo

import (
	"log"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/hud"
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

func weapon(class string, size, count int) []elite.Module {
	mods := make([]elite.Module, count)
	for i := range mods {
		mods[i] = elite.Module{Name: class, Class: class, Size: size}
	}
	return mods
}

// fireLists: fire group lists as the HUD would have shown them.
func fireLists(key int, secondary string, secondaryUtility bool, primary string, primaryUtility bool) map[int][2]hud.FireList {
	return map[int][2]hud.FireList{key: {
		hud.Secondary: {Known: true, Key: key, Names: []string{secondary}, Counts: []int{1}, Classes: []string{secondary}, Utility: secondaryUtility},
		hud.Primary:   {Known: true, Key: key, Names: []string{primary}, Counts: []int{1}, Classes: []string{primary}, Utility: primaryUtility},
	}}
}

func steps(client backend.Output) []step {
	return []step{
		{"Normal space, hull 100% (green)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
		}, nil},
		{"Hardpoints deployed: clack, R2/L2 weapon click", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut, 0)
			play(h, "hardpoints", n)
		}, nil},
		{"FIRE: R2 = large multi-cannons (right: a rising whir while they spin up, then the rattle), L2 = beam lasers (left); hold them", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Modules = append(weapon("multicannon", 3, 2), weapon("beam", 2, 1)...)
			status(g, n, weaponsOut, 0)
		}, nil},
		{"FIRE: R2 = rail guns (hold to charge, release), L2 = missiles (press)", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Modules = append(weapon("railgun", 2, 2), weapon("missile", 1, 1)...)
			status(g, n, weaponsOut, 0)
		}, nil},
		{"FIRE: R2 = pulse lasers, L2 = plasma accelerator; hold them", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Modules = append(weapon("pulse", 3, 2), weapon("plasma", 4, 1)...)
			status(g, n, weaponsOut, 0)
		}, nil},
		{"TURNING: turn the controller or push the left stick: felt as turn_feel says, outside the throttle's blue zone...", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			g.HUD.BlueZone = hud.Tracked[bool]{Value: false, OK: true, At: n.Add(7 * time.Second)}
		}, nil},
		{"... and nothing inside it, where the ship turns best. A flick adds a soft push; a finger on the touchpad stops the gyro part", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			g.HUD.BlueZone = hud.Tracked[bool]{Value: true, OK: true, At: n.Add(7 * time.Second)}
		}, nil},
		{"MENU TEST: the gyro is OFF for 6 s; move the controller, the mouse must not move", 6 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			client.SetMotion(client.Controllers(), backend.MotionDisabled)
		}, func() {
			client.SetMotion(client.Controllers(), backend.MotionProfile)
			log.Print("   gyro back on")
		}},
		{"THRUST: hold R1 (light rumble), press Circle to BOOST", 6 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
		}, nil},
		{"Fire group 3 (player LEDs) and a tick", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.OnStatus(elite.Status{Flags: weaponsOut, FireGroup: 2}, n)
			play(h, "fire_group", n)
			play(h, "pips", n.Add(600*time.Millisecond))
		}, nil},
		{"Hull 50% (amber)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Hull = 0.5
			status(g, n, weaponsOut, 0)
		}, nil},
		{"Hull 15% (red)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Hull = 0.15
			status(g, n, weaponsOut, 0)
		}, nil},
		{"Taking hits: thumps and red flashes", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Hull = 0.8
			status(g, n, weaponsOut, 0)
			for i := range 3 {
				at := n.Add(time.Duration(i) * 1200 * time.Millisecond)
				g.Moments = append(g.Moments, game.Moment{Kind: game.Attacked, At: at})
				play(h, "hull_hit", at)
			}
			h.OnEvent(elite.Event{"event": "UnderAttack", "Target": "You"}, g, n)
		}, nil},
		{"Scanned by another ship: a scan line sweeps from left to right", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			event(g, h, n, elite.Event{"event": "Scanned", "ScanType": "Cargo"})
		}, nil},
		{"Kill confirmed: double tap, green flash", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			event(g, h, n, elite.Event{"event": "Bounty"})
		}, nil},
		{"Combat music: the lightbar breathes", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Music = "Combat_Dogfight"
			status(g, n, weaponsOut, 0)
		}, nil},
		{"Shields down: a heavy hit, red blinking, the hull rattling", 5 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			event(g, h, n, elite.Event{"event": "ShieldState", "ShieldsUp": false})
			status(g, n, elite.InMainShip|elite.HardpointsDeployed|elite.InDanger, 0)
		}, nil},
		{"HEAT (estimate): hold R2 and L2 (beam lasers), the throb speeds up as heat builds", 8 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Modules = weapon("beam", 2, 2)
			status(g, n, weaponsOut, 0)
			h.SetHeat(0.4)
		}, nil},
		{"Overheating (over 100%): orange blinking, boiling", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut|elite.Overheating, 0)
		}, nil},
		{"Scanners out (analysis mode): light feedback", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut|elite.AnalysisMode, 0)
		}, nil},
		{"FSD charging: the lightbar rises (jump_feel \"calm\" adds a soft pulse every second)", 5 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			status(g, n, ship|elite.FSDCharging, 0)
		}, nil},
		{"Hyperspace jump: a soft swell into the tunnel, then quiet; the LEDs count down", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.HyperspaceStart = n.Add(-5 * time.Second) // the countdown is over
			status(g, n, ship|elite.FSDJump, 0)
		}, nil},
		{"Supercruise entry (blue)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Supercruise, 0)
			play(h, "supercruise_in", n)
		}, nil},
		{"Being interdicted: magenta, a strong rumble", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Supercruise|elite.BeingInterdicted, 0)
		}, nil},
		{"Supercruise drop", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "supercruise_out", n)
		}, nil},
		{"Fuel scooping: amber breathing, a light rumble", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Supercruise|elite.ScoopingFuel, 0)
		}, nil},
		{"Landing gear, low fuel (the mic LED pulses)", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.LowFuel|elite.LandingGearDown, 0)
			play(h, "landing_gear", n)
		}, nil},
		{"Silent running: the lightbar off, the mic LED on", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.SilentRunning, 0)
			play(h, "silent_running", n)
		}, nil},
		{"Docked: the clamps thump, the lights dim", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship|elite.Docked, 0)
			play(h, "docked", n)
		}, nil},
		{"FLIGHT ASSIST off, then on", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "fa_off", n)
			play(h, "fa_on", n.Add(1500*time.Millisecond))
		}, nil},
		{"FSD cooldown over (right), mass lock in, mass lock out", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "fsd_ready", n)
			play(h, "mass_lock", n.Add(1300*time.Millisecond))
			play(h, "mass_unlock", n.Add(2600*time.Millisecond))
		}, nil},
		{"ATMOSPHERE: gliding in", 5 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, elite.GlideMode)
			play(h, "glide_start", n)
		}, nil},
		{"GROUND RUSH: dropping fast near the surface", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			g.SetDescent(500, 110, n.Add(4*time.Second))
		}, nil},
		{"HEAT SINK, CHAFF, SHIELD CELL (from your Elite bindings in the game)", 6 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "heat_sink", n)
			play(h, "chaff", n.Add(1500*time.Millisecond))
			play(h, "shield_cell", n.Add(3*time.Second))
		}, nil},
		{"CARGO: a canister scooped, a canister ejected", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "cargo_collect", n)
			play(h, "cargo_eject", n.Add(1500*time.Millisecond))
		}, nil},
		{"MESSAGE from a player or your wing", 2 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "message", n)
		}, nil},
		{"THARGOID: a slow throb, the shutdown field (lights out), the reboot", 10 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.Music = "Unknown_Encounter"
			status(g, n, ship, 0)
			g.StartShutdown(n, 4*time.Second)
			play(h, "systems_shutdown", n)
		}, nil},
		{"SHIELD HITS (read from the HUD): left, right, then sustained fire and the low-shield crackle", 7 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			h.Play(haptics.Shot{Effect: "shield_hit", At: n, Scale: 0.8, Pan: -1})
			h.Play(haptics.Shot{Effect: "shield_hit", At: n.Add(900 * time.Millisecond), Scale: 0.8, Pan: 1})
			h.Play(haptics.Shot{Effect: "shield_hit", At: n.Add(1800 * time.Millisecond)})
			play(h, "shield_regen", n.Add(2600*time.Millisecond))
			play(h, "shield_regen", n.Add(3100*time.Millisecond))
			g.HUD.Shield = hud.Tracked[int]{Value: 18, OK: true, At: n.Add(7 * time.Second)}
			g.HUD.Splash = 3.5
		}, nil},
		{"HEAT (read from the HUD): passing 60, 70, 80%", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			g.HUD.Heat = hud.Tracked[int]{Value: 82, OK: true, At: n.Add(4 * time.Second)}
			for i := range 3 {
				h.Play(haptics.Shot{Effect: "heat_notch", At: n.Add(time.Duration(i) * 800 * time.Millisecond), Scale: 0.4 + 0.2*float64(i)})
			}
		}, nil},
		{"HULL HITS (read from the HUD): metallic blows, then the hull creaking at 15%", 6 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			h.Play(haptics.Shot{Effect: "hull_hit_hud", At: n, Scale: 0.7})
			h.Play(haptics.Shot{Effect: "hull_hit_hud", At: n.Add(1100 * time.Millisecond)})
			g.HUD.Hull = hud.Tracked[int]{Value: 15, OK: true, At: n.Add(6 * time.Second)}
		}, nil},
		{"YOUR SHOTS LANDING: ticks on the R2 side, then L2, then the target's shields breaking", 5 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut, 0)
			for i := range 6 {
				side := haptics.RightSide
				if i >= 3 {
					side = haptics.LeftSide
				}
				h.Play(haptics.Shot{Effect: "target_hit", At: n.Add(time.Duration(i) * 300 * time.Millisecond), Side: side, Scale: 0.7})
			}
			play(h, "target_shield_break", n.Add(2600*time.Millisecond))
		}, nil},
		{"BOOST with an empty engine capacitor: a dud, then a real boost", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, ship, 0)
			play(h, "boost_empty", n)
			play(h, "boost", n.Add(1500*time.Millisecond))
		}, nil},
		{"WEAPONS CAPACITOR EMPTY while firing: the triggers go slack (hold R2)", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			status(g, n, weaponsOut, 0)
			g.FiredAt = n.Add(4 * time.Second)
			g.HUD.Capacitors = hud.Tracked[[3]float64]{Value: [3]float64{1, 1, 0}, OK: true, At: n.Add(4 * time.Second)}
		}, nil},
		{"FIRE GROUP FROM THE HUD: R2 = 2 beam lasers, L2 = 3 multi-cannons, as the lists show; hold them", 6 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			// the loadout alone would guess the other way round (more multi-cannons)
			g.Modules = append(weapon("multicannon", 3, 3), weapon("beam", 2, 2)...)
			g.FireLists = fireLists(hud.FireKey(0, true), "multicannon", false, "beam", false)
			status(g, n, weaponsOut, 0)
		}, nil},
		{"RELOAD (read from the HUD): hold L2; the clips drop out, the multi-cannons fall silent, L2 goes slack...", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.FireLists = fireLists(hud.FireKey(0, true), "multicannon", false, "beam", false)
			status(g, n, weaponsOut, 0)
			g.HUD.Lists[hud.Secondary] = hud.FireList{At: n.Add(3 * time.Second), Ammo: 3, Reloading: 3}
			h.Play(haptics.Shot{Effect: "reload", At: n, Side: haptics.LeftSide})
		}, nil},
		{"... and the new clips seat: firing again", 3 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.FireLists = fireLists(hud.FireKey(0, true), "multicannon", false, "beam", false)
			status(g, n, weaponsOut, 0)
			g.HUD.Lists[hud.Secondary] = hud.FireList{At: n.Add(3 * time.Second), Ammo: 3}
			h.Play(haptics.Shot{Effect: "reload_done", At: n, Side: haptics.LeftSide})
		}, nil},
		{"UTILITIES ON THE TRIGGERS (hardpoints in): L2 = heat sink (press), R2 = kill warrant scanner (hold)", 6 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			g.FireLists = fireLists(hud.FireKey(0, false), "heatsink", true, "scanner", true)
			status(g, n, ship, 0)
		}, nil},
		{"On foot, health 60%: R2 weapon, L2 aim", 4 * time.Second, func(g *game.State, h *haptics.Engine, n time.Time) {
			health := 0.6
			g.OnStatus(elite.Status{Flags2: elite.OnFoot | elite.OnFootOnPlanet, Health: &health}, n)
		}, nil},
	}
}
