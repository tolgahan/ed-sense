// Package game keeps what EDSense knows about the running game: the latest
// Status.json, what the journal said, and what the HUD shows.
package game

import (
	"math"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/hud"
)

// hyperspaceCountdown: StartJump(Hyperspace) to FSDJump, measured in journals.
const hyperspaceCountdown = 18 * time.Second

// A Thargoid shutdown field kills the ship's systems for about 30 s. Elite
// logs only the start, so the reboot is timed.
const (
	shutdownSilence = 27 * time.Second
	shutdownReboot  = 3 * time.Second
)

type ShutdownPhase int

const (
	NoShutdown ShutdownPhase = iota
	SystemsDead
	Rebooting
)

// State is the game as EDSense sees it. The fields are set by OnStatus,
// OnEvent and the app; the demo sets them directly.
type State struct {
	Status      elite.Status
	HaveStatus  bool
	Music       string
	Hull        float64 // 0-1, of the ship or fighter flown
	MainMenu    bool    // a new journal starts at the main menu; LoadGame leaves it
	Closed      bool    // the game logged Shutdown
	ShieldsSeen bool    // the ship has shields: no shields-down alarm on shieldless builds

	FSDChargeStart  time.Time
	HyperspaceStart time.Time // a hyperspace jump is counting down
	DiedAt          time.Time
	ShutdownAt      time.Time // Thargoid shutdown field
	Moments         []Moment  // recent live events worth a flash or a buzz, oldest first

	Modules   []elite.Module          // fire-groupable, from the latest Loadout
	FireLists map[int][2]hud.FireList // what the HUD showed, by hud.FireKey

	HUD     hud.State // the latest HUD reading
	FiredAt time.Time // a fire trigger was last pulled

	altitude altitude
}

// altitude tracks the height over a planet's surface and how fast it drops.
type altitude struct {
	metres  float64
	descent float64 // m/s, positive going down, smoothed
	ok      bool
	at      time.Time
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
	g.trackAltitude(s, now)
	g.Status, g.HaveStatus = s, true
}

// trackAltitude: Altitude is from the surface unless
// AltitudeFromAverageRadius is set (high up).
func (g *State) trackAltitude(s elite.Status, now time.Time) {
	a := &g.altitude
	f := s.Flags
	if s.Altitude == nil || !f.Has(elite.HasLatLong) || f.Has(elite.AltitudeFromAverageRadius) || f.Has(elite.Supercruise) || f.Has(elite.Docked|elite.Landed) {
		a.ok, a.descent = false, 0
		return
	}
	metres := *s.Altitude
	if !a.ok {
		a.descent = 0
	} else if dt := now.Sub(a.at).Seconds(); dt >= 0.05 && dt < 5 {
		v := (a.metres - metres) / dt
		a.descent += (v - a.descent) * math.Min(1, dt/0.6)
	}
	a.metres, a.ok, a.at = metres, true, now
}

// Descent returns the altitude and how fast it drops, while fresh
// (Status.json only changes when something changes).
func (g *State) Descent(now time.Time) (metres, rate float64, ok bool) {
	a := g.altitude
	if !a.ok || now.Sub(a.at) > 2500*time.Millisecond {
		return 0, 0, false
	}
	return a.metres, a.descent, true
}

// SetDescent sets the altitude and the descent rate, fresh until the given time.
func (g *State) SetDescent(metres, rate float64, until time.Time) {
	g.altitude = altitude{metres: metres, descent: rate, ok: true, at: until.Add(-2500 * time.Millisecond)}
}

func (g *State) ShutdownPhase(now time.Time) ShutdownPhase {
	if g.ShutdownAt.IsZero() {
		return NoShutdown
	}
	switch el := now.Sub(g.ShutdownAt); {
	case el < shutdownSilence:
		return SystemsDead
	case el < shutdownSilence+shutdownReboot:
		return Rebooting
	}
	return NoShutdown
}

// RebootProgress: how far the reboot after a shutdown is, 0-1.
func (g *State) RebootProgress(now time.Time) float64 {
	return now.Sub(g.ShutdownAt.Add(shutdownSilence)).Seconds() / shutdownReboot.Seconds()
}

// StartShutdown puts the ship in a shutdown field at now, its systems dead
// for the given time before the reboot.
func (g *State) StartShutdown(now time.Time, dead time.Duration) {
	g.ShutdownAt = now.Add(dead - shutdownSilence)
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

// InMenu reports the main menu or one of the given GuiFocus panels.
func (g *State) InMenu(panels []int) bool {
	switch {
	case g.Closed:
		return false
	case g.MainMenu:
		return true
	case !g.HaveStatus:
		return false
	}
	for _, p := range panels {
		if p != elite.NoPanel && g.Status.GuiFocus == p {
			return true
		}
	}
	return false
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
		g.Closed, g.MainMenu = false, true
	case "LoadGame":
		g.Closed, g.MainMenu = false, false
		if g.Music == elite.MainMenuMusic {
			g.Music = ""
		}
	case "Shutdown":
		g.Closed, g.MainMenu = true, false
	case "Music":
		g.Music = ev.Text("MusicTrack")
		if g.Music == elite.MainMenuMusic {
			g.MainMenu = true
		}
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
		g.DiedAt, g.ShutdownAt = time.Time{}, time.Time{}
	case "Repair":
		if repairsHull(ev) {
			g.Hull = 1
		}
	case "Died":
		if live {
			g.DiedAt = now
		}
		g.ShutdownAt = time.Time{}
	case "SystemsShutdown":
		if live {
			g.ShutdownAt = now
		}
	case "Docked", "Touchdown":
		g.ShutdownAt = time.Time{}
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
	mods := elite.LoadoutModules(ev)
	if !sameModules(mods, g.Modules) {
		g.FireLists = nil // another ship or a refit: the lists are read again
	}
	g.Modules = mods
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

func sameModules(a, b []elite.Module) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Utility != b[i].Utility {
			return false
		}
	}
	return true
}
