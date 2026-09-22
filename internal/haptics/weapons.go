package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

// fireTrigger is a trigger and what firing with it feels like.
type fireTrigger struct {
	list   int // game.Primary or game.Secondary
	side   Side
	effect string
}

var fireTriggers = [2]fireTrigger{
	{game.Primary, RightSide, "fire_primary"},
	{game.Secondary, LeftSide, "fire_secondary"},
}

func (t fireTrigger) pulled(pad dualsense.State) bool {
	if t.list == game.Primary {
		return pad.R2Held()
	}
	return pad.L2Held()
}

// triggerState is kept per list.
type triggerState struct {
	held     [2][2]time.Time // held since, per weapon class on the trigger
	spin     [2][2]float64   // spin-up, 0-1, per weapon class on the trigger
	firingAt [2]time.Time    // last fired
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
		if m.Class == class && m.Size > 0 && (size == 0 || m.Size < size) {
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
		set := sets[t.list]
		down := weapons && pulled
		// up to two weapon classes on a trigger, the second a bit softer
		for k, class := range set.Classes[:min(2, len(set.Classes))] {
			firing, spin := e.spinUp(t, k, class, down, dt, g.Modules)
			if firing {
				e.triggers.firingAt[t.list] = now
				heatIn += weaponHeat(class) / float64(k+1)
			}
			if !e.native {
				if k == 0 && firing {
					m.add(t.effect, t.effect, Voice{}, flutter)
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
			e.fire(now, class, effect, t, k, firing, 1/float64(k+1), m)
		}
	}
	if pad.Held(dualsense.R1) {
		m.add("thrust", "thrust", Voice{Wave: Noise, F0: 90, Amp: 0.35}, 1)
		m.add("thrust_low", "", Voice{Wave: Sine, F0: 32, Amp: 0.25}, 1)
	}
	if !e.boostFromBindings && pad.WasPressed(dualsense.Circle) && !s.Flags.Has(elite.Supercruise) {
		e.playNow("boost", now)
	}
	e.turning(now, dt, pad, s.Flags.Has(elite.FlightAssistOff), m)
	return heatIn
}

// fire is the feel of one weapon class on a trigger (k: which class on it;
// level: how strongly it is felt).
func (e *Engine) fire(now time.Time, class, effect string, t fireTrigger, k int, down bool, level float64, m *mix) {
	held := &e.triggers.held[t.list][k]
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
			m.add(effect, "", v, level)
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
			m.add(effect, "", v, level)
		}
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
