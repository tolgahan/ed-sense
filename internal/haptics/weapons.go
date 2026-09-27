package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/hud"
)

// fireTrigger is a trigger and what firing with it feels like.
type fireTrigger struct {
	list   int // hud.Primary or hud.Secondary
	side   Side
	effect string
}

var fireTriggers = [2]fireTrigger{
	{hud.Primary, RightSide, "fire_primary"},
	{hud.Secondary, LeftSide, "fire_secondary"},
}

func (t fireTrigger) pulled(pad dualsense.State) bool {
	if t.list == hud.Primary {
		return pad.R2Held()
	}
	return pad.L2Held()
}

// triggerState is kept per list.
type triggerState struct {
	down      [2]bool         // pulled last tick
	held      [2][2]time.Time // held since, per weapon class on the trigger
	spin      [2][2]float64   // spin-up, 0-1, per weapon class on the trigger
	firingAt  [2]time.Time    // last fired
	utilityAt [2]time.Time    // last utility feel
}

// spinUp follows a weapon class's spin-up and reports whether it fires.
// Multi-cannons spin their barrels up before the first shot, longer the
// bigger they are, and spin down when released: a quick second press fires
// sooner.
func (e *Engine) spinUp(t fireTrigger, k int, class string, down bool, dt float64, mods []elite.Module) (firing bool, spin float64) {
	s := &e.triggers.spin[t.list][k]
	d := e.spinUpTime(class, mods).Seconds()
	switch {
	case d <= 0:
		*s = 0
		return down, 1
	case down:
		*s = math.Min(1, *s+dt/d)
	default:
		*s = math.Max(0, *s-dt/d)
	}
	return down && *s >= 1, *s
}

// spinUpTime: multi-cannons only. With several on the trigger's ship, the
// smallest fires first.
func (e *Engine) spinUpTime(class string, mods []elite.Module) time.Duration {
	if class != "multicannon" {
		return 0
	}
	size := 0
	for _, m := range mods {
		if m.Class == class && !m.Utility && m.Size > 0 && (size == 0 || m.Size < size) {
			size = m.Size
		}
	}
	return time.Duration(e.cfg.SpinUpMs[sizeName(size)]) * time.Millisecond
}

// sizeName: the hardpoint size as the settings name it; medium when unknown.
func sizeName(size int) string {
	switch size {
	case 1:
		return "small"
	case 3:
		return "large"
	case 4:
		return "huge"
	}
	return "medium"
}

// flying: what the controller does while flying (weapons, thrust, boost and
// turning). It returns the heat the weapons add per second.
func (e *Engine) flying(now time.Time, dt float64, g *game.State, pad dualsense.State, m *mix) (heatIn float64) {
	s := g.Status
	weapons := s.Flags.Has(elite.HardpointsDeployed) && !s.Flags.Has(elite.AnalysisMode)
	sets := g.FireSets(e.cfg.FireGroups)
	flutter := 0.85 + 0.15*math.Sin(2*math.Pi*14*seconds(now))
	for _, t := range fireTriggers {
		pulled := t.pulled(pad)
		pressed := pulled && !e.triggers.down[t.list]
		e.triggers.down[t.list] = pulled
		set := sets[t.list]
		if set.Utility {
			if !s.Flags.Has(elite.AnalysisMode) {
				e.utilityFire(now, set.Classes, t, pulled, pressed, m)
			}
			continue
		}
		down := weapons && pulled
		share := g.FiringShare(t.list, now) // weapons reloading fall silent
		// up to two weapon classes on a trigger, the second a bit softer
		for k, class := range set.Classes[:min(2, len(set.Classes))] {
			firing, spin := e.spinUp(t, k, class, down, dt, g.Modules)
			if firing {
				e.triggers.firingAt[t.list] = now
				heatIn += weaponHeat(class) / float64(k+1)
			}
			if !e.native {
				if k == 0 && firing && share > 0 {
					m.add(t.effect, t.effect, Voice{}, flutter*share)
				}
				continue
			}
			effect := t.effect
			if k > 0 {
				effect += "_2"
			}
			if down && !firing {
				// the barrels winding up: a smooth rising whir
				v := Voice{Wave: Sine, F0: 60 + 60*spin, Amp: 0.12 + 0.23*spin}
				v.L, v.R = t.side.gains()
				m.add(effect+"_spin", "", v, 1)
			}
			e.fire(now, class, effect, t, k, firing, share/float64(k+1), m)
		}
	}
	if pad.Held(dualsense.R1) {
		m.add("thrust", "thrust", Voice{Wave: Noise, F0: 90, Amp: 0.35}, 1)
		m.add("thrust_low", "", Voice{Wave: Sine, F0: 32, Amp: 0.25}, 1)
	}
	if !e.boostFromBindings && pad.WasPressed(dualsense.Circle) && !s.Flags.Has(elite.Supercruise) {
		e.startBoost(now, g)
	}
	e.learnBoost(now, g)
	e.turning(now, dt, pad, s.Flags.Has(elite.FlightAssistOff), g, m)
	return heatIn
}

// fire is the feel of one weapon class on a trigger (k: which class on it;
// share: how much of it is firing).
func (e *Engine) fire(now time.Time, class, effect string, t fireTrigger, k int, down bool, share float64, m *mix) {
	held := &e.triggers.held[t.list][k]
	if share <= 0 {
		down = false
	}
	switch class {
	case "railgun":
		// charges while held, cracks on release
		if down {
			if held.IsZero() {
				*held = now
			}
			p := math.Min(1, now.Sub(*held).Seconds())
			v := Voice{Wave: Sine, F0: 60 + 160*p, Amp: 0.3 + 0.5*p, TremHz: 4 + 20*p, TremDepth: 0.3}
			v.L, v.R = t.side.gains()
			m.add(effect, "", v, share)
		} else if !held.IsZero() {
			if now.Sub(*held) > 250*time.Millisecond {
				e.play(Shot{Effect: "rail_crack", At: now, Side: t.side, Scale: 1})
			}
			*held = time.Time{}
		}
	case "missile":
		if !down {
			*held = time.Time{}
		} else if held.IsZero() {
			e.play(Shot{Effect: "missile_launch", At: now, Side: t.side, Scale: 1})
			*held = now
		}
	default:
		if down {
			v := WeaponTexture(class)
			v.L, v.R = t.side.gains()
			m.add(effect, "", v, share)
		}
	}
}

// utilityFire: a trigger that fires utilities only.
func (e *Engine) utilityFire(now time.Time, classes []string, t fireTrigger, pulled, pressed bool, m *mix) {
	if len(classes) == 0 {
		return
	}
	once := func(effect string, gap time.Duration) {
		if pressed && now.Sub(e.triggers.utilityAt[t.list]) > gap {
			e.playNow(effect, now)
			e.triggers.utilityAt[t.list] = now
		}
	}
	switch classes[0] {
	case "heatsink":
		if pressed && now.Sub(e.triggers.utilityAt[t.list]) > 2*time.Second {
			e.heat = math.Min(e.heat, 0.1) // a heat sink takes the heat away
		}
		once("heat_sink", 2*time.Second)
	case "chaff":
		once("chaff", time.Second)
	case "shieldcell":
		once("shield_cell", 3*time.Second)
	case "ecm":
		once("ecm", 2*time.Second)
	case "limpet":
		once("limpet", 300*time.Millisecond)
	case "scanner":
		// scanners work while held: a slow sweeping hum on that side
		if pulled {
			v := Voice{Wave: Sine, F0: 110, Amp: 0.3, TremHz: 2.5, TremDepth: 0.6}
			v.L, v.R = t.side.gains()
			m.add("scanner", "scanner", v, 1)
		}
	case "pointdefence":
		// fires on its own
	default:
		once("utility", 200*time.Millisecond)
	}
}

// onFootShots: a shot on each R2 press, repeating while held for automatic
// weapons.
func (e *Engine) onFootShots(now time.Time, s elite.Status, pad dualsense.State) {
	shot := onFootShot(s.SelectedWeapon)
	if shot == "" {
		shot = "shot_kinetic"
	}
	period := map[string]time.Duration{"shot_kinetic": 125 * time.Millisecond, "shot_laser": 200 * time.Millisecond, "shot_plasma": 700 * time.Millisecond}[shot]
	if !pad.WasPressed(dualsense.R2) && (pad.R2 <= 180 || now.Sub(e.lastShot) < period) {
		return
	}
	if e.native {
		e.play(Shot{Effect: shot, At: now, Side: RightSide, Scale: 1})
	} else {
		e.playNow("onfoot_shot", now)
	}
	e.lastShot = now
}

// weaponHeat: estimated heat per second while a weapon class fires.
func weaponHeat(class string) float64 {
	switch class {
	case "beam":
		return 0.07
	case "plasma", "railgun":
		return 0.05
	case "mining", "burst":
		return 0.04
	case "pulse":
		return 0.03
	case "cannon", "fragment":
		return 0.015
	}
	return 0.01
}
