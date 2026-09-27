package app

import (
	"log"
	"path/filepath"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/hud"
)

// setHUDPalette gives the HUD reader the colours to look for.
func (s *session) setHUDPalette() {
	if s.hud == nil {
		return
	}
	dataDir := s.dataDir()
	pal, source, matrixKey := hud.ResolvePalette(s.cfg.HUDColors, dataDir)
	s.hud.SetPalette(pal, !hud.ManualColours(s.cfg.HUDColors), func(p hud.Palette) {
		if err := hud.SaveLearned(dataDir, matrixKey, p); err != nil {
			log.Printf("HUD: could not save the colours found: %v", err)
		}
	})
	flash := pal.Flash.Hex()
	if pal.NoFlash {
		flash = "off"
	}
	log.Printf("HUD colours: %s (shield %s, heat %s, flame %s, hit flash %s)", source, pal.Shield.Hex(), pal.Heat.Hex(), pal.Flame.Hex(), flash)
}

// readHUD runs the HUD reader in the cockpit with the game in front (the
// reader checks that), and passes on what it read.
func (s *session) readHUD(now time.Time, active bool) {
	if s.hud == nil {
		return
	}
	st := s.game.Status
	on := s.cfg.HUDReader && active && st.InShip() && !st.Flags.Has(elite.Docked|elite.FSDJump) && !st.InPanel() &&
		s.game.ShutdownPhase(now) == game.NoShutdown
	debugDir := ""
	if s.cfg.HUDDebug {
		debugDir = filepath.Join(s.dataDir(), "hud_debug")
	}
	s.hud.SetActive(on, st.Flags.Has(elite.ShieldsUp), debugDir)
	// the fire group lists: with the hardpoints out, or in with a fight on or
	// a trigger pulled lately (utilities)
	hardpoints := st.Flags.Has(elite.HardpointsDeployed)
	lists := on && st.Flags.Has(elite.InMainShip) && !st.Flags.Has(elite.AnalysisMode) &&
		(hardpoints || st.Flags.Has(elite.InDanger) || now.Sub(s.triggerAt) < 10*time.Second)
	s.hud.SetFireGroups(lists, hud.FireKey(st.FireGroup, hardpoints), hardpoints, s.triggersHeld, now)
	if !on {
		return
	}
	hs := s.hud.Take()
	for _, ev := range hs.Events {
		if ev.Kind == hud.ShieldHit {
			s.lastHUDHit = now
		}
	}
	// full speed while a fight is on or the shields or heat move, calmer
	// while cruising
	s.hud.SetFast(hardpoints || st.Flags.Has(elite.InDanger) || !st.Flags.Has(elite.ShieldsUp) ||
		now.Sub(s.lastHUDHit) < 20*time.Second ||
		hs.Shield.OK && hs.Shield.Value < 100 || hs.Heat.OK && hs.Heat.Value >= 50)
	s.game.LearnFireLists(hs)
	s.game.HUD = hs
	s.haptics.OnHUD(hs.Events, s.game, now)
}

// targetChanged: another target's numbers start over; scan stages and
// subsystems of the same ship keep them.
func (s *session) targetChanged(ev elite.Event) {
	if s.hud == nil {
		return
	}
	key := ""
	if ev.Bool("TargetLocked") {
		key = ev.Text("Ship") + "|" + ev.Text("PilotName")
	}
	if key != s.target || key == "" {
		s.hud.ResetTarget()
	}
	s.target = key
}
