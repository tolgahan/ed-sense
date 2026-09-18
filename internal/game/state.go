// Package game keeps what EDSense knows about the running game: the latest
// Status.json and what the journal said.
package game

import (
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
)

// hyperspaceCountdown: StartJump(Hyperspace) to FSDJump, measured in journals.
const hyperspaceCountdown = 18 * time.Second

// State is the game as EDSense sees it. The fields are set by OnStatus and
// OnEvent.
type State struct {
	Status      elite.Status
	HaveStatus  bool
	Music       string
	Hull        float64 // 0-1, of the ship or fighter flown
	Closed      bool    // the game logged Shutdown
	ShieldsSeen bool    // the ship has shields: no shields-down alarm on shieldless builds

	FSDChargeStart  time.Time
	HyperspaceStart time.Time // a hyperspace jump is counting down
	DiedAt          time.Time
	Moments         []Moment // recent live events worth a flash or a buzz, oldest first
}

func New() *State { return &State{Hull: 1} }

func (g *State) OnStatus(s elite.Status, now time.Time) {
	if s.Flags.Has(elite.FSDCharging) && !(g.HaveStatus && g.Status.Flags.Has(elite.FSDCharging)) {
		g.FSDChargeStart = now
	}
	if !s.Flags.Has(elite.FSDJump) && !g.HyperspaceStart.IsZero() && now.Sub(g.HyperspaceStart) > hyperspaceCountdown+5*time.Second {
		g.HyperspaceStart = time.Time{}
	}
	if s.Flags.Has(elite.ShieldsUp) {
		g.ShieldsSeen = true
	}
	g.Status, g.HaveStatus = s, true
}

// HyperspaceCountdown: while a hyperspace jump counts down, the part of the
// countdown left (0-1).
func (g *State) HyperspaceCountdown(now time.Time) (left float64, counting bool) {
	if g.HyperspaceStart.IsZero() {
		return 0, false
	}
	rem := hyperspaceCountdown - now.Sub(g.HyperspaceStart)
	if rem <= 0 {
		return 0, false
	}
	return rem.Seconds() / hyperspaceCountdown.Seconds(), true
}

// Active reports whether the player is in the game: not in the main menu,
// not closed.
func (g *State) Active() bool {
	if !g.HaveStatus || g.Closed || g.Music == elite.MainMenuMusic {
		return false
	}
	return g.Status.Flags != 0 || g.Status.Flags2 != 0
}

// Context is a short description of the situation, for the log.
func (g *State) Context() string {
	s := g.Status
	switch {
	case !g.Active():
		return "not in game"
	case s.OnFoot():
		return "on foot"
	case s.Flags.Has(elite.InSRV):
		return "SRV"
	case s.Flags.Has(elite.Docked):
		return "docked"
	case s.Flags.Has(elite.Landed):
		return "landed"
	case s.Flags.Has(elite.FSDJump):
		return "hyperspace"
	case s.Flags.Has(elite.Supercruise):
		return "supercruise"
	case s.Flags.Has(elite.HardpointsDeployed) && s.Flags.Has(elite.AnalysisMode):
		return "normal space, scanners out"
	case s.Flags.Has(elite.HardpointsDeployed):
		return "normal space, weapons out"
	default:
		return "normal space"
	}
}

// InCombat: combat music, or danger.
func (g *State) InCombat() bool {
	return elite.IsCombatMusic(g.Music) || g.Status.Flags.Has(elite.InDanger)
}

// OnEvent applies a journal event. live is false while the history is read
// at startup: the state is rebuilt, and no moments or times are recorded.
func (g *State) OnEvent(ev elite.Event, live bool, now time.Time) {
	switch ev.Name() {
	case "Fileheader":
		g.Closed = false
	case "LoadGame":
		g.Closed = false
		if g.Music == elite.MainMenuMusic {
			g.Music = ""
		}
	case "Shutdown":
		g.Closed = true
	case "Music":
		g.Music = ev.Text("MusicTrack")
	case "Loadout":
		g.onLoadout(ev)
	case "HullDamage":
		// only the hull of whatever the player flies
		if ev.Bool("Fighter") != g.Status.Flags.Has(elite.InFighter) {
			return
		}
		if h, ok := ev.Number("Health"); ok {
			if live && h < g.Hull-0.001 {
				g.remember(HullDamaged, now)
			}
			g.Hull = h
		}
	case "RepairAll", "Resurrect":
		g.Hull = 1
		g.DiedAt = time.Time{}
	case "Repair":
		if repairsHull(ev) {
			g.Hull = 1
		}
	case "Died":
		if live {
			g.DiedAt = now
		}
	case "StartJump":
		if live && ev.Text("JumpType") == "Hyperspace" {
			g.HyperspaceStart = now
		}
	case "FSDJump", "SupercruiseExit", "SupercruiseEntry":
		g.HyperspaceStart = time.Time{}
	}
	if live {
		if m, ok := momentOf(ev); ok {
			g.remember(m, now)
		}
	}
}

func (g *State) onLoadout(ev elite.Event) {
	if h, ok := ev.Number("HullHealth"); ok {
		g.Hull = h
	}
	g.ShieldsSeen = g.Status.Flags.Has(elite.ShieldsUp)
}

func repairsHull(ev elite.Event) bool {
	items := strings.ToLower(ev.Text("Item"))
	if list, ok := ev["Items"].([]any); ok {
		for _, it := range list {
			if s, ok := it.(string); ok {
				items += " " + strings.ToLower(s)
			}
		}
	}
	return strings.Contains(items, "hull") || strings.Contains(items, "wear") || strings.Contains(items, "all")
}
