package lights

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/hud"
)

// buzz: the triggers' answer to a moment.
type buzz struct {
	trigger string
	dur     time.Duration
}

var buzzes = map[game.MomentKind]buzz{
	game.Attacked:     {"hit", 250 * time.Millisecond},
	game.JetConeBoost: {"jet_cone", time.Second},
}

func (r *Renderer) trigger(key string) dsx.Trigger {
	t := r.cfg.TriggerFX[key]
	return dsx.NewTrigger(t.Mode, t.Params)
}

// pair: the effects named prefix_l and prefix_r.
func (r *Renderer) pair(prefix string) (left, right dsx.Trigger) {
	return r.trigger(prefix + "_l"), r.trigger(prefix + "_r")
}

// triggers: the adaptive trigger effects for the situation.
func (r *Renderer) triggers(g *game.State, now time.Time) (left, right dsx.Trigger) {
	s := g.Status
	left, right = dsx.TriggerNone(), dsx.TriggerNone()
	switch {
	case s.InPanel():
	case s.OnFoot():
		if !s.Flags2.Has(elite.OnFootSocialSpace | elite.OnFootInStation | elite.OnFootInHangar) {
			left, right = r.pair("onfoot")
		}
	case s.Flags.Has(elite.InSRV):
		if s.Flags.Has(elite.SRVTurretView) {
			left, right = r.pair("srv_turret")
		}
	case s.InShip() && !s.Parked():
		left, right = r.shipTriggers(g, now)
	}
	if b, ok := latestBuzz(g.Moments, now); ok && !s.InPanel() && !s.Parked() && !s.Flags.Has(elite.BeingInterdicted) && !g.FSDCharging(now) {
		t := r.trigger(b.trigger)
		left, right = t, t
	}
	return left, right
}

func (r *Renderer) shipTriggers(g *game.State, now time.Time) (left, right dsx.Trigger) {
	s := g.Status
	weapons := s.Flags.Has(elite.HardpointsDeployed) && !s.Flags.Has(elite.AnalysisMode)
	switch {
	case s.Flags.Has(elite.BeingInterdicted):
		t := r.trigger("interdiction")
		return t, t
	case g.FSDCharging(now):
		// a vibration that grows as the drive charges
		amp := 2 + int(math.Min(6, now.Sub(g.FSDChargeStart).Seconds()*6/5))
		v := dsx.Trigger{Mode: dsx.TriggerVibration, Params: []int{1, amp, 20}}
		return v, v
	case weapons && s.Flags.Has(elite.Overheating):
		return r.pair("ship_overheat")
	case weapons && r.slack(g, now):
		return r.pair("ship_wep_empty")
	case weapons:
		left, right = r.pair("ship_weapons")
		// every weapon on a trigger reloading: that trigger goes slack
		if g.FiringShare(hud.Primary, now) == 0 {
			right = r.trigger("ship_reload_r")
		}
		if g.FiringShare(hud.Secondary, now) == 0 {
			left = r.trigger("ship_reload_l")
		}
		return left, right
	case s.Flags.Has(elite.HardpointsDeployed):
		return r.pair("ship_scanner")
	}
	return dsx.TriggerNone(), dsx.TriggerNone()
}

// latestBuzz: the buzz of the latest moment still giving one.
func latestBuzz(moments []game.Moment, now time.Time) (buzz, bool) {
	for i := len(moments) - 1; i >= 0; i-- {
		m := moments[i]
		if b, ok := buzzes[m.Kind]; ok && !now.Before(m.At) && now.Before(m.At.Add(b.dur)) {
			return b, true
		}
	}
	return buzz{}, false
}
