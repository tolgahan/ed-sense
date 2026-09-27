package hud

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// processor turns per-frame readings into steady values and events.
type processor struct {
	state State

	shield, heat, hull             valueFilter
	lastShield, lastHeat, lastHull int // last steady values, -1 before the first
	splash                         float64
	lastHit, lastRegen             time.Time

	target    targetTracker
	blueZone  []bool
	caps      [][3]float64
	fireLists fireLists
}

type targetTracker struct {
	shield, hull         valueFilter
	lastShield, lastHull int // -1: none yet
	splash               float64
	lastHit              time.Time
	noShield             int  // reads in a row with the hull % but no shield %
	broken               bool // shield break felt; again once the shield % is back
	seen                 time.Time
}

func newProcessor() *processor {
	return &processor{
		shield:     valueFilter{near: 3, need: 3, minScore: 0.6, max: 100},
		heat:       valueFilter{near: 4, need: 3, minScore: 0.6, max: 199},
		hull:       valueFilter{near: 6, need: 3, minScore: 0.68, max: 100}, // hull hits come in bigger steps
		lastShield: -1,
		lastHeat:   -1,
		lastHull:   -1,
		target: targetTracker{
			shield:     valueFilter{near: 4, need: 3, minScore: 0.45, max: 100},
			hull:       valueFilter{near: 5, need: 4, minScore: 0.55, max: 100},
			lastShield: -1,
			lastHull:   -1,
		},
	}
}

// feed takes one frame's reading.
func (p *processor) feed(r Reading, now time.Time) {
	p.state.Reads++
	p.feedShield(r, now)
	p.feedHeat(r.Heat, now)
	p.feedHull(r.Hull, now)
	p.feedTarget(r.Target, now)
	if r.BlueZoneSeen {
		p.feedBlueZone(r.InBlueZone, now)
	}
	if r.CapsOK {
		p.feedCapacitors(r.Caps, now)
	}
	for list, rd := range r.Lists {
		p.feedList(list, rd, now)
	}
}

func (p *processor) feedShield(r Reading, now time.Time) {
	if !r.Shield.Found() {
		p.state.Splash, p.splash = 0, 0
		return
	}
	p.state.ShieldReads++
	v, ok := p.shield.feed(r.Shield)
	if ok {
		p.state.Shield = Tracked[int]{Value: v, OK: true, At: now}
	}
	// a hit flash appearing on the hologram
	hit := r.Splash-p.splash > 0.35 || (r.Splash > 0.8 && p.splash < 0.3)
	if hit && now.Sub(p.lastHit) > 120*time.Millisecond {
		p.emitHit(Event{Kind: ShieldHit, Strength: math.Min(1, 0.3+r.Splash/4), Pan: math.Max(-1, math.Min(1, r.SplashPan))}, now)
	}
	p.state.Splash, p.splash = r.Splash, r.Splash
	if !ok {
		return
	}
	// the number dropping (hits the flash missed) or refilling
	if p.lastShield >= 0 {
		switch d := p.lastShield - v; {
		case d > 0 && now.Sub(p.lastHit) > 300*time.Millisecond:
			p.emitHit(Event{Kind: ShieldHit, Strength: math.Min(1, 0.25+float64(d)/12)}, now)
		case d < 0 && now.Sub(p.lastRegen) > 400*time.Millisecond:
			p.emit(Event{Kind: ShieldRegen, Strength: 1})
			p.lastRegen = now
		}
	}
	p.lastShield = v
}

func (p *processor) feedHeat(n Number, now time.Time) {
	if !n.Found() {
		return
	}
	v, ok := p.heat.feed(n)
	if !ok {
		return
	}
	p.state.Heat = Tracked[int]{Value: v, OK: true, At: now}
	if p.lastHeat >= 0 && v > p.lastHeat {
		for notch := 60; notch <= 150; notch += 10 {
			if p.lastHeat < notch && v >= notch {
				p.emit(Event{Kind: HeatNotch, Strength: math.Min(1, float64(notch-40)/60)})
			}
		}
	}
	p.lastHeat = v
}

func (p *processor) feedHull(n Number, now time.Time) {
	if !n.Found() {
		return
	}
	v, ok := p.hull.feed(n)
	if !ok {
		return
	}
	p.state.Hull = Tracked[int]{Value: v, OK: true, At: now}
	if p.lastHull >= 0 && v < p.lastHull {
		p.emit(Event{Kind: HullHit, Strength: math.Min(1, 0.4+float64(p.lastHull-v)/10)})
	}
	p.lastHull = v
}

func (p *processor) feedTarget(r Target, now time.Time) {
	t := &p.target
	if !r.Found() {
		t.splash = 0
		if now.Sub(t.seen) > 3*time.Second {
			p.resetTarget()
		}
		return
	}
	t.seen = now
	hit := false
	// hit flashes on its hologram
	if r.Splash-t.splash > 0.35 || (r.Splash > 0.8 && t.splash < 0.3) {
		if now.Sub(t.lastHit) > 90*time.Millisecond {
			p.emit(Event{Kind: TargetHit, Strength: math.Min(1, 0.4+r.Splash/5)})
			t.lastHit, hit = now, true
		}
	}
	t.splash = r.Splash
	switch {
	case r.Shield.Found():
		t.noShield, t.broken = 0, false
		if v, ok := t.shield.feed(r.Shield); ok {
			p.state.TargetShield = Tracked[int]{Value: v, OK: true, At: now}
			if t.lastShield >= 0 && v < t.lastShield && !hit && now.Sub(t.lastHit) > 150*time.Millisecond {
				p.emit(Event{Kind: TargetHit, Strength: math.Min(1, 0.3+float64(t.lastShield-v)/10)})
				t.lastHit = now
			}
			t.lastShield = v
		}
	case r.ShieldsDown:
		t.noShield++
		// shields were low, now only the hull shows: they collapsed
		if t.noShield == 3 && !t.broken && t.lastShield >= 0 && t.lastShield <= 40 {
			p.emit(Event{Kind: TargetShieldBreak, Strength: 1})
			t.broken = true
			p.state.TargetShield.Value, t.lastShield = 0, 0
		}
	}
	if !r.Hull.Found() {
		return
	}
	if v, ok := t.hull.feed(r.Hull); ok {
		p.state.TargetHull = Tracked[int]{Value: v, OK: true, At: now}
		// hull hits land once its shields are down
		if t.lastHull >= 0 && v < t.lastHull && t.noShield >= 3 && now.Sub(t.lastHit) > 90*time.Millisecond {
			p.emit(Event{Kind: TargetHullHit, Strength: math.Min(1, 0.4+float64(t.lastHull-v)/8)})
			t.lastHit = now
		}
		t.lastHull = v
	}
}

// resetTarget: a new target, or none; the old one's numbers are forgotten.
func (p *processor) resetTarget() {
	t := &p.target
	t.shield.reset()
	t.hull.reset()
	t.lastShield, t.lastHull = -1, -1
	t.noShield, t.broken = 0, false
	p.state.TargetShield.OK, p.state.TargetHull.OK = false, false
}

// feedBlueZone: in the blue zone when two of the last three reads say so.
func (p *processor) feedBlueZone(in bool, now time.Time) {
	p.blueZone = append(p.blueZone, in)
	if len(p.blueZone) > 3 {
		p.blueZone = p.blueZone[1:]
	}
	n := 0
	for _, b := range p.blueZone {
		if b {
			n++
		}
	}
	p.state.BlueZone = Tracked[bool]{Value: n*2 > len(p.blueZone), OK: true, At: now}
}

// feedCapacitors: the median of the last five reads; single reads are noisy.
func (p *processor) feedCapacitors(caps [3]float64, now time.Time) {
	p.caps = append(p.caps, caps)
	if len(p.caps) > 5 {
		p.caps = p.caps[1:]
	}
	if len(p.caps) < 3 {
		return
	}
	var median [3]float64
	for i := range median {
		v := make([]float64, len(p.caps))
		for k, c := range p.caps {
			v[k] = c[i]
		}
		median[i] = vision.Percentile(v, 50)
	}
	p.state.Capacitors = Tracked[[3]float64]{Value: median, OK: true, At: now}
}

func (p *processor) emit(e Event) { p.state.Events = append(p.state.Events, e) }

func (p *processor) emitHit(e Event, now time.Time) {
	p.emit(e)
	p.lastHit = now
}

// lost: the HUD could not be read (menus, looking around, game not in
// front). The values are kept and go stale; the hit splash is cleared.
func (p *processor) lost() { p.state.Splash, p.splash = 0, 0 }

// take returns the state and clears the events.
func (p *processor) take() State {
	s := p.state
	p.state.Events = nil
	return s
}
