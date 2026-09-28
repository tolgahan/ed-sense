package game

import (
	"slices"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/hud"
)

var testStart = time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)

const ship = elite.InMainShip | elite.ShieldsUp

func TestInMenu(t *testing.T) {
	g := New()
	panels := config.Default().GyroOffGuiFocus
	now := testStart
	g.OnEvent(elite.Event{"event": "Fileheader"}, true, now)
	if !g.InMenu(panels) {
		t.Fatal("a new journal starts at the main menu")
	}
	g.OnEvent(elite.Event{"event": "LoadGame"}, true, now)
	g.OnStatus(elite.Status{Flags: ship}, now)
	if g.InMenu(panels) {
		t.Fatal("the cockpit is not a menu")
	}
	for panel, want := range map[int]bool{1: true, 5: true, 6: true, 9: false, 10: false, 11: true} {
		g.OnStatus(elite.Status{Flags: ship, GuiFocus: panel}, now)
		if got := g.InMenu(panels); got != want {
			t.Errorf("GuiFocus %d: menu %v, want %v", panel, got, want)
		}
	}
	g.OnStatus(elite.Status{Flags: ship}, now)
	g.OnEvent(elite.Event{"event": "Music", "MusicTrack": "MainMenu"}, true, now)
	if !g.InMenu(nil) || g.Active() {
		t.Fatal("main menu music is the main menu")
	}
	g.OnEvent(elite.Event{"event": "LoadGame"}, true, now)
	if g.InMenu(nil) || !g.Active() {
		t.Fatal("LoadGame leaves the main menu")
	}
	g.OnEvent(elite.Event{"event": "Shutdown"}, true, now)
	if g.InMenu(panels) || g.Active() {
		t.Fatal("a closed game is not a menu")
	}
	g.OnStatus(elite.Status{}, now)
	if g.Active() {
		t.Fatal("no flags: not in the game")
	}
}

func TestHullOfTheShipFlown(t *testing.T) {
	g := New()
	g.OnStatus(elite.Status{Flags: ship}, testStart)
	g.OnEvent(elite.Event{"event": "HullDamage", "Health": 0.3, "Fighter": true}, true, testStart)
	if g.Hull != 1 || len(g.Moments) != 0 {
		t.Fatalf("fighter damage changed the ship: hull %v, moments %v", g.Hull, g.Moments)
	}
	g.OnEvent(elite.Event{"event": "HullDamage", "Health": 0.6, "Fighter": false}, true, testStart)
	if g.Hull != 0.6 || len(g.Moments) != 1 || g.Moments[0].Kind != HullDamaged {
		t.Fatalf("hull %v, moments %v", g.Hull, g.Moments)
	}
	g.OnEvent(elite.Event{"event": "RepairAll"}, true, testStart)
	if g.Hull != 1 {
		t.Fatal("repaired")
	}
}

func TestMoments(t *testing.T) {
	g := New()
	g.OnEvent(elite.Event{"event": "ShieldState", "ShieldsUp": false}, false, testStart)
	if len(g.Moments) != 0 {
		t.Fatal("history makes no moments")
	}
	g.OnEvent(elite.Event{"event": "ShieldState", "ShieldsUp": false}, true, testStart)
	g.OnEvent(elite.Event{"event": "UnderAttack", "Target": "Fighter"}, true, testStart)
	g.OnEvent(elite.Event{"event": "UnderAttack", "Target": "You"}, true, testStart)
	if kinds := []MomentKind{g.Moments[0].Kind, g.Moments[1].Kind}; len(g.Moments) != 2 || kinds[0] != ShieldsLost || kinds[1] != Attacked {
		t.Fatalf("moments %v", g.Moments)
	}
	g.OnEvent(elite.Event{"event": "Bounty"}, true, testStart.Add(time.Minute))
	if len(g.Moments) != 1 || g.Moments[0].Kind != Kill {
		t.Fatalf("old moments kept: %v", g.Moments)
	}
}

func TestDescent(t *testing.T) {
	g := New()
	now := testStart
	for alt := 2000.0; alt > 1500; alt -= 50 { // falling at 100 m/s
		a := alt
		g.OnStatus(elite.Status{Flags: ship | elite.HasLatLong, Altitude: &a}, now)
		now = now.Add(500 * time.Millisecond)
	}
	if _, rate, ok := g.Descent(now.Add(-500 * time.Millisecond)); !ok || rate < 80 || rate > 120 {
		t.Fatalf("descent rate %v %v", rate, ok)
	}
	if _, _, ok := g.Descent(now.Add(5 * time.Second)); ok {
		t.Fatal("a stale altitude")
	}
	high := 9000.0
	g.OnStatus(elite.Status{Flags: ship | elite.HasLatLong | elite.AltitudeFromAverageRadius, Altitude: &high}, now)
	if _, _, ok := g.Descent(now); ok {
		t.Fatal("no surface altitude high up")
	}
}

func TestShutdownPhases(t *testing.T) {
	g := New()
	g.OnEvent(elite.Event{"event": "SystemsShutdown"}, true, testStart)
	for _, c := range []struct {
		after time.Duration
		want  ShutdownPhase
	}{{time.Second, SystemsDead}, {shutdownSilence + time.Second, Rebooting}, {shutdownSilence + shutdownReboot + time.Second, NoShutdown}} {
		if got := g.ShutdownPhase(testStart.Add(c.after)); got != c.want {
			t.Errorf("after %v: %v, want %v", c.after, got, c.want)
		}
	}
	g.StartShutdown(testStart, 4*time.Second)
	if g.ShutdownPhase(testStart.Add(3*time.Second)) != SystemsDead || g.ShutdownPhase(testStart.Add(5*time.Second)) != Rebooting {
		t.Fatal("StartShutdown")
	}
}

func TestHyperspaceCountdown(t *testing.T) {
	g := New()
	g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, testStart)
	if left, ok := g.HyperspaceCountdown(testStart.Add(time.Second)); !ok || left < 0.9 {
		t.Fatalf("start: %v %v", left, ok)
	}
	if _, ok := g.HyperspaceCountdown(testStart.Add(hyperspaceCountdown)); ok {
		t.Fatal("counted down")
	}
	// the jump and charging flags are on from the countdown
	g.OnStatus(elite.Status{Flags: elite.FSDJump | elite.FSDCharging}, testStart)
	if _, ok := g.HyperspaceTunnel(testStart.Add(4 * time.Second)); ok || !g.FSDCharging(testStart.Add(4*time.Second)) {
		t.Fatal("counting down, still charging")
	}
	if d, ok := g.HyperspaceTunnel(testStart.Add(6 * time.Second)); !ok || d != time.Second {
		t.Fatalf("in the tunnel for 1 s: %v %v", d, ok)
	}
	if g.FSDCharging(testStart.Add(6 * time.Second)) {
		t.Fatal("no charging in the tunnel")
	}
	g.OnEvent(elite.Event{"event": "FSDJump"}, true, testStart.Add(time.Second))
	if _, ok := g.HyperspaceCountdown(testStart.Add(2 * time.Second)); ok {
		t.Fatal("jumped")
	}
	if _, ok := g.HyperspaceTunnel(testStart.Add(2 * time.Second)); ok {
		t.Fatal("arrived")
	}
}

func kraitLoadout() elite.Event {
	return elite.Event{"event": "Loadout", "Modules": []any{
		map[string]any{"Slot": "LargeHardpoint1", "Item": "hpt_multicannon_gimbal_large", "AmmoInClip": 77.0, "AmmoInHopper": 2100.0},
		map[string]any{"Slot": "LargeHardpoint2", "Item": "hpt_multicannon_gimbal_large", "AmmoInClip": 77.0, "AmmoInHopper": 2100.0},
		map[string]any{"Slot": "LargeHardpoint3", "Item": "hpt_multicannon_gimbal_large", "AmmoInClip": 77.0, "AmmoInHopper": 1680.0},
		map[string]any{"Slot": "MediumHardpoint1", "Item": "hpt_beamlaser_gimbal_medium"},
		map[string]any{"Slot": "MediumHardpoint2", "Item": "hpt_beamlaser_gimbal_medium"},
		map[string]any{"Slot": "TinyHardpoint1", "Item": "hpt_shieldbooster_size0_class5"},
		map[string]any{"Slot": "TinyHardpoint3", "Item": "hpt_heatsinklauncher_turret_tiny", "AmmoInClip": 1.0, "AmmoInHopper": 2.0},
	}}
}

func classes(sets [2]FireSet) (primary, secondary string) {
	return sets[hud.Primary].Classes[0], sets[hud.Secondary].Classes[0]
}

// The fire groups: the settings, else what the HUD showed, else a guess
// from the loadout (3 multi-cannons, 2 beams: the multi-cannons on R2).
func TestFireSets(t *testing.T) {
	g := New()
	g.OnStatus(elite.Status{Flags: ship | elite.HardpointsDeployed}, testStart)
	if p, s := classes(g.FireSets(nil)); p != "generic" || s != "generic" {
		t.Fatalf("no loadout: %s / %s", p, s)
	}
	g.OnEvent(kraitLoadout(), false, testStart)
	if p, s := classes(g.FireSets(nil)); p != "multicannon" || s != "beam" {
		t.Fatalf("guess: %s / %s", p, s)
	}
	key := hud.FireKey(0, true)
	lists := hud.State{}
	lists.Lists[hud.Secondary] = hud.FireList{Known: true, Key: key, Names: []string{"MULTI-CANNON"}, Counts: []int{3}, Classes: []string{"multicannon"}}
	lists.Lists[hud.Primary] = hud.FireList{Known: true, Key: key, Names: []string{"BEAM LASER"}, Counts: []int{2}, Classes: []string{"beam"}, Energy: 2}
	g.LearnFireLists(lists)
	if p, s := classes(g.FireSets(nil)); p != "beam" || s != "multicannon" {
		t.Fatalf("from the HUD: R2 %s, L2 %s", p, s)
	}
	g.Status.FireGroup = 1
	if p, _ := classes(g.FireSets(nil)); p != "multicannon" {
		t.Fatalf("fire group 2 is not known yet: %s", p)
	}
	g.Status.FireGroup = 0
	settings := map[string]config.FireGroup{"1": {Primary: "pulse", Secondary: "auto"}}
	if p, s := classes(g.FireSets(settings)); p != "pulse" || s != "multicannon" {
		t.Fatalf("with settings: R2 %s, L2 %s", p, s)
	}
	// another ship: the lists are read again
	g.OnEvent(elite.Event{"event": "Loadout", "Modules": []any{
		map[string]any{"Slot": "MediumHardpoint1", "Item": "hpt_railgun_fixed_medium"},
	}}, false, testStart)
	if g.FireLists != nil || !slices.Equal(g.FireSets(nil)[hud.Primary].Classes, []string{"railgun"}) {
		t.Fatal("new ship")
	}
}

func TestFiringShare(t *testing.T) {
	g := New()
	g.OnStatus(elite.Status{Flags: ship | elite.HardpointsDeployed}, testStart)
	key := hud.FireKey(0, true)
	g.FireLists = map[int][2]hud.FireList{key: {
		hud.Secondary: {Known: true, Key: key, Classes: []string{"multicannon"}},
		hud.Primary:   {Known: true, Key: key, Classes: []string{"beam"}, Energy: 2},
	}}
	now := testStart
	g.HUD.Lists[hud.Secondary] = hud.FireList{At: now, Ammo: 3, Reloading: 1}
	if s := g.FiringShare(hud.Secondary, now); s < 0.66 || s > 0.67 {
		t.Fatalf("one of three reloading: %.2f", s)
	}
	g.HUD.Lists[hud.Secondary] = hud.FireList{At: now, Ammo: 3, Reloading: 3}
	if s := g.FiringShare(hud.Secondary, now); s != 0 {
		t.Fatalf("all reloading: %.2f", s)
	}
	if s := g.FiringShare(hud.Secondary, now.Add(2*time.Second)); s != 1 {
		t.Fatalf("a stale read: %.2f", s)
	}
}
