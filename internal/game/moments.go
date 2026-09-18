package game

import (
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
)

// Moment is a live event the lights and triggers answer with a short flash
// or buzz.
type Moment struct {
	Kind MomentKind
	At   time.Time
}

type MomentKind int

const (
	HullDamaged MomentKind = iota
	ShieldsRaised
	ShieldsLost
	Attacked // UnderAttack, aimed at the player
	Kill
	HeatWarning
	JetConeBoost
	Interdicted
	DockingGranted
	DockingDenied
)

// momentMemory: moments are kept this long, longer than any flash.
const momentMemory = 5 * time.Second

func momentOf(ev elite.Event) (MomentKind, bool) {
	switch ev.Name() {
	case "ShieldState":
		if ev.Bool("ShieldsUp") {
			return ShieldsRaised, true
		}
		return ShieldsLost, true
	case "UnderAttack":
		return Attacked, ev.Text("Target") == "You"
	case "Bounty", "FactionKillBond", "CapShipBond":
		return Kill, true
	case "HeatWarning":
		return HeatWarning, true
	case "JetConeBoost":
		return JetConeBoost, true
	case "Interdicted":
		return Interdicted, true
	case "DockingGranted":
		return DockingGranted, true
	case "DockingDenied":
		return DockingDenied, true
	}
	return 0, false
}

// remember records a moment and forgets old ones.
func (g *State) remember(kind MomentKind, at time.Time) {
	kept := g.Moments[:0]
	for _, m := range g.Moments {
		if at.Sub(m.At) < momentMemory {
			kept = append(kept, m)
		}
	}
	g.Moments = append(kept, Moment{kind, at})
}
