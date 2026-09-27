package hud

import "time"

// Tracked is a value from the HUD and when it was last read.
type Tracked[T any] struct {
	Value T
	OK    bool      // a value is known
	At    time.Time // when it was last read
}

// Fresh reports whether the value is known and was read within maxAge.
func (t Tracked[T]) Fresh(now time.Time, maxAge time.Duration) bool {
	return t.OK && now.Sub(t.At) < maxAge
}

// State is the steady view of the HUD.
type State struct {
	Shield, Heat, Hull       Tracked[int]
	TargetShield, TargetHull Tracked[int]
	Splash                   float64 // hit flashes on the ship hologram now (sustained fire)
	BlueZone                 Tracked[bool]
	Capacitors               Tracked[[3]float64] // SYS, ENG, WEP charge 0-1
	Lists                    [2]FireList         // Secondary, Primary

	Events []Event // since the last Take

	Reads       int // frames read
	ShieldReads int // ... with the shield % found
}

// FireList is what a fire group list shows.
type FireList struct {
	Known   bool     // Names are known for fire group Key
	Key     int      // see FireKey
	Names   []string // modules on the list, most entries first
	Counts  []int
	Classes []string // weapon classes, most entries first; utility classes when there are no weapons
	Utility bool     // Classes are utility classes
	Energy  int      // weapons without ammo

	At        time.Time // last read with the list in view
	Ammo      int       // weapons with ammo in that read
	Reloading int       // ... and how many of them were reloading
}

// FireKey identifies what a list shows: the fire group and whether the
// hardpoints are out.
func FireKey(fireGroup int, hardpoints bool) int {
	k := fireGroup * 2
	if hardpoints {
		k++
	}
	return k
}

// EventKind is something on the HUD the haptics react to.
type EventKind string

const (
	ShieldHit         EventKind = "shield_hit"
	ShieldRegen       EventKind = "shield_regen"
	HeatNotch         EventKind = "heat_notch" // heat passing 60, 70, 80 ... %
	HullHit           EventKind = "hull_hit"
	TargetHit         EventKind = "target_hit"
	TargetShieldBreak EventKind = "target_shield_break"
	TargetHullHit     EventKind = "target_hull_hit"
	ReloadStart       EventKind = "reload_start"
	ReloadDone        EventKind = "reload_done"
)

// Side is the side of the controller an event belongs to.
type Side int

const (
	BothSides Side = iota
	LeftSide       // L2, the SECONDARY list
	RightSide      // R2, the PRIMARY list
)

// ListSide is the side of a list (Secondary or Primary).
func ListSide(list int) Side { return Side(list + 1) }

type Event struct {
	Kind     EventKind
	Strength float64 // 0-1
	Pan      float64 // -1 left .. +1 right
	Side     Side
}
