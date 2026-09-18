package game

import (
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
)

var testStart = time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)

const ship = elite.InMainShip | elite.ShieldsUp

func TestActive(t *testing.T) {
	g := New()
	now := testStart
	g.OnEvent(elite.Event{"event": "Fileheader"}, true, now)
	if g.Active() {
		t.Fatal("no status yet")
	}
	g.OnEvent(elite.Event{"event": "LoadGame"}, true, now)
	g.OnStatus(elite.Status{Flags: ship}, now)
	if !g.Active() {
		t.Fatal("in the cockpit")
	}
	g.OnEvent(elite.Event{"event": "Music", "MusicTrack": "MainMenu"}, true, now)
	if g.Active() {
		t.Fatal("main menu music is the main menu")
	}
	g.OnEvent(elite.Event{"event": "LoadGame"}, true, now)
	if !g.Active() {
		t.Fatal("LoadGame leaves the main menu")
	}
	g.OnEvent(elite.Event{"event": "Shutdown"}, true, now)
	if g.Active() {
		t.Fatal("a closed game is not active")
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

func TestHyperspaceCountdown(t *testing.T) {
	g := New()
	g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, testStart)
	if left, ok := g.HyperspaceCountdown(testStart.Add(time.Second)); !ok || left < 0.9 {
		t.Fatalf("start: %v %v", left, ok)
	}
	if _, ok := g.HyperspaceCountdown(testStart.Add(hyperspaceCountdown)); ok {
		t.Fatal("counted down")
	}
	g.OnEvent(elite.Event{"event": "FSDJump"}, true, testStart.Add(time.Second))
	if _, ok := g.HyperspaceCountdown(testStart.Add(2 * time.Second)); ok {
		t.Fatal("jumped")
	}
}
