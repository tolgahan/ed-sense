package hud

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func num(text string, score float64) Number { return Number{Text: text, Score: score} }

func kinds(events []Event) map[EventKind]int {
	m := map[EventKind]int{}
	for _, e := range events {
		m[e.Kind]++
	}
	return m
}

func TestValueFilter(t *testing.T) {
	f := valueFilter{near: 3, need: 3, minScore: 0.6, max: 100}
	feed := func(text string, score float64) int {
		v, _ := f.feed(num(text, score))
		return v
	}
	for range 3 {
		feed("85%", 0.8)
	}
	if v, ok := f.feed(num("85%", 0.8)); !ok || v != 85 {
		t.Fatal("85 after three reads")
	}
	if feed("83%", 0.8) != 83 {
		t.Fatal("a small drop is taken at once")
	}
	if feed("63%", 0.9) != 83 || feed("83%", 0.9) != 83 {
		t.Fatal("one odd read is ignored")
	}
	if feed("84%", 0.9) != 84 || feed("88%", 0.9) != 84 {
		t.Fatal("rises one step at a time")
	}
	if feed("70%", 0.3) != 84 {
		t.Fatal("a low-confidence read is ignored")
	}
	for range 3 {
		feed("70%", 0.8)
	}
	if feed("70%", 0.8) != 70 {
		t.Fatal("a big drop is taken once confirmed")
	}
	// "4%" at 70: probably a hidden first digit, so it needs more reads
	for range 4 {
		feed("4%", 0.9)
	}
	if feed("70%", 0.9) != 70 {
		t.Fatal("a one-digit read needs more confirmation")
	}
}

func TestShieldAndHeatEvents(t *testing.T) {
	p := newProcessor()
	now := testStart
	step := func(r Reading) State {
		now = now.Add(100 * time.Millisecond)
		p.feed(r, now)
		return p.take()
	}
	base := Reading{Shield: num("90%", 0.9), Heat: num("55%", 0.9)}
	for range 4 {
		step(base)
	}
	r := base
	r.Splash, r.SplashPan = 2.0, -0.8
	if st := step(r); len(st.Events) != 1 || st.Events[0].Kind != ShieldHit || st.Events[0].Pan >= 0 {
		t.Fatalf("a hit on the left expected, got %+v", st.Events)
	}
	if st := step(r); len(st.Events) != 0 || st.Splash != 2.0 {
		t.Fatalf("a sustained flash is not a new hit every frame: %+v", st)
	}
	r = base
	r.Heat = num("56%", 0.9)
	step(r)
	notch := false
	for _, h := range []string{"57%", "58%", "59%", "60%", "61%"} {
		r.Heat = num(h, 0.9)
		notch = notch || kinds(step(r).Events)[HeatNotch] > 0
	}
	if !notch {
		t.Fatal("no heat notch at 60%")
	}
	// the shield % dropping without a flash is a hit too
	r = base
	r.Shield = num("88%", 0.9)
	now = now.Add(time.Second)
	if st := step(r); len(st.Events) != 1 || st.Events[0].Kind != ShieldHit {
		t.Fatalf("drop without a flash: %+v", st.Events)
	}
}

func TestPanelEvents(t *testing.T) {
	p := newProcessor()
	now := testStart
	step := func(r Reading) map[EventKind]int {
		now = now.Add(100 * time.Millisecond)
		p.feed(r, now)
		return kinds(p.take().Events)
	}
	// hull: steady at 100, then a hit to 96
	for range 4 {
		step(Reading{Hull: num("100%", 0.85)})
	}
	if ev := step(Reading{Hull: num("96%", 0.85)}); ev[HullHit] != 1 {
		t.Fatalf("hull drop: %v", ev)
	}
	if st := p.take(); !st.Hull.OK || st.Hull.Value != 96 {
		t.Fatalf("hull %+v", st.Hull)
	}

	// the target: shields at 30%, then a flash on its hologram
	target := func(shield string, splash float64) Reading {
		return Reading{Target: Target{Shield: num(shield, 0.7), Hull: num("100%", 0.7), Splash: splash}}
	}
	bare := func(hull string) Reading {
		return Reading{Target: Target{Hull: num(hull, 0.7), ShieldsDown: true}}
	}
	for range 4 {
		step(target("30%", 0))
	}
	if ev := step(target("30%", 1.5)); ev[TargetHit] != 1 {
		t.Fatalf("target flash: %v", ev)
	}
	step(target("30%", 1.4)) // the same flash fading
	if ev := step(target("28%", 1.3)); ev[TargetHit] != 1 {
		t.Fatalf("target shield drop: %v", ev)
	}
	// shields collapse: only the hull % is left, three reads in a row
	breaks := 0
	for range 4 {
		breaks += step(bare("100%"))[TargetShieldBreak]
	}
	if breaks != 1 {
		t.Fatalf("shield breaks felt: %d", breaks)
	}
	for range 3 {
		step(bare("100%"))
	}
	if ev := step(bare("97%")); ev[TargetHullHit] != 1 {
		t.Fatalf("target hull hit: %v", ev)
	}
	// a new target that never had low shields: no break
	p.resetTarget()
	for range 5 {
		if step(bare("80%"))[TargetShieldBreak] > 0 {
			t.Fatal("a break without shields seen")
		}
	}

	// blue zone: two of the last three reads
	for _, in := range []bool{true, false, true} {
		step(Reading{BlueZoneSeen: true, InBlueZone: in})
	}
	if st := p.take(); !st.BlueZone.OK || !st.BlueZone.Value {
		t.Fatal("blue zone")
	}
	// capacitors: the median of the last five reads
	for _, c := range [][3]float64{{1, 1, 0}, {1, 0.2, 0.05}, {0.9, 1, 1}, {1, 1, 0}, {1, 1, 0.05}} {
		step(Reading{CapsOK: true, Caps: c})
	}
	if st := p.take(); !st.Capacitors.OK || st.Capacitors.Value != [3]float64{1, 1, 0.05} {
		t.Fatalf("capacitors %+v", st.Capacitors)
	}
}

// entryFor: a list entry as the reader would see the module.
func entryFor(i int, sub SubLine) Entry {
	m := kraitLoadout()[i]
	e := Entry{G: 20, Width: entryWidth(m), Icon: !m.Utility, Sub: sub, Clip: 0.6}
	if sub == AmmoLine {
		e.SubWidth = textWidth(m.AmmoText)
	}
	return e
}

const (
	multiCannon = 0 // kraitLoadout indexes
	beamLaser   = 3
	heatSink    = 5
)

func TestFireListsFromReads(t *testing.T) {
	left := ListRead{OK: true, Entries: []Entry{
		entryFor(multiCannon, AmmoLine), entryFor(heatSink, AmmoLine), entryFor(multiCannon, AmmoLine),
		entryFor(heatSink, AmmoLine), entryFor(multiCannon, AmmoLine),
	}}
	right := ListRead{OK: true, Entries: []Entry{entryFor(beamLaser, NoSubLine), entryFor(beamLaser, NoSubLine)}}

	p := newProcessor()
	p.setModules(kraitLoadout())
	now := testStart
	p.setFireKey(FireKey(0, true), true, now)
	feed := func() {
		for range 4 {
			p.feedList(Secondary, left, now)
			p.feedList(Primary, right, now)
			now = now.Add(250 * time.Millisecond)
		}
	}
	feed()
	if p.state.Lists[Secondary].Known || p.state.Lists[Primary].Known {
		t.Fatal("lists taken while the fire group was just set")
	}
	feed()
	st := p.take()
	if l := st.Lists[Secondary]; !l.Known || l.String() != "MULTI-CANNON x3, HEATSINK x2" || strings.Join(l.Classes, ",") != "multicannon" || l.Utility {
		t.Fatalf("left list: %+v", l)
	}
	if l := st.Lists[Primary]; !l.Known || l.String() != "BEAM LASER x2" || strings.Join(l.Classes, ",") != "beam" || l.Energy != 2 {
		t.Fatalf("right list: %+v", l)
	}

	// one wrong entry in one read does not change the list
	p = newProcessor()
	p.setModules(kraitLoadout())
	p.setFireKey(FireKey(0, true), true, testStart)
	now = testStart.Add(2 * time.Second)
	odd := ListRead{OK: true, Entries: append(append([]Entry(nil), right.Entries...), entryFor(multiCannon, AmmoLine))}
	for i, r := range []ListRead{right, odd, right, right} {
		p.feedList(Primary, r, now.Add(time.Duration(i)*250*time.Millisecond))
	}
	if l := p.state.Lists[Primary]; l.String() != "BEAM LASER x2" {
		t.Fatalf("after one odd read: %s", l)
	}
}

// Once shown, a list holds through reads that miss entries or add a wrong
// one, and follows a change that lasts.
func TestFireListsHold(t *testing.T) {
	mc, hs, beam := entryFor(multiCannon, AmmoLine), entryFor(heatSink, AmmoLine), entryFor(beamLaser, NoSubLine)
	full := ListRead{OK: true, Entries: []Entry{mc, hs, mc, hs, mc}}
	noCannons := ListRead{OK: true, Entries: []Entry{hs, hs}}
	twoCannons := ListRead{OK: true, Entries: []Entry{mc, hs, mc, hs}}
	withBeam := ListRead{OK: true, Entries: []Entry{mc, hs, mc, beam, mc, hs}}

	p := newProcessor()
	p.setModules(kraitLoadout())
	now := testStart
	p.setFireKey(FireKey(0, true), true, now)
	now = now.Add(2 * time.Second)
	feed := func(reads ...ListRead) string {
		for _, r := range reads {
			p.feedList(Secondary, r, now)
			now = now.Add(300 * time.Millisecond)
		}
		return p.state.Lists[Secondary].String()
	}
	want := "MULTI-CANNON x3, HEATSINK x2"
	if l := feed(full, full, full, full, full); l != want {
		t.Fatalf("steady reads: %s", l)
	}
	for i, r := range []ListRead{noCannons, twoCannons, withBeam, noCannons, full, twoCannons, noCannons, withBeam, full, noCannons, twoCannons, full} {
		if l := feed(r); l != want {
			t.Fatalf("read %d: %s", i, l)
		}
	}
	if l := feed(noCannons, noCannons, noCannons, noCannons, noCannons, noCannons, noCannons); l != "HEATSINK x2" {
		t.Fatalf("the multi-cannons taken out of the fire group: %s", l)
	}
}

func TestReloadEvents(t *testing.T) {
	p := newProcessor()
	p.setModules(kraitLoadout())
	now := testStart
	p.setFireKey(FireKey(0, true), true, now)
	ready := entryFor(multiCannon, AmmoLine)
	busy := ready
	busy.Sub, busy.Clip, busy.SubWidth = StatusLine, 0, 7.6
	read := func(list int, es ...Entry) string {
		p.feedList(list, ListRead{OK: true, Entries: es}, now)
		now = now.Add(100 * time.Millisecond)
		var ev []string
		for _, e := range p.take().Events {
			ev = append(ev, fmt.Sprintf("%s/%d", e.Kind, e.Side))
		}
		return strings.Join(ev, " ")
	}
	if ev := read(Secondary, busy, busy, busy); ev != "" {
		t.Fatalf("DEPLOYING just after the hardpoints came out is not a reload: %v", ev)
	}
	now = now.Add(3 * time.Second)
	read(Secondary, ready, ready, ready)
	if ev := read(Secondary, busy, ready, ready); ev != "reload_start/1" {
		t.Fatalf("reload start: %v", ev)
	}
	if l := p.state.Lists[Secondary]; l.Reloading != 1 || l.Ammo != 3 {
		t.Fatalf("list %+v", l)
	}
	if ev := read(Secondary, ready, ready); ev != "" {
		t.Fatalf("the reloading weapon out of view is not the end of the reload: %v", ev)
	}
	if ev := read(Secondary, busy, ready, ready); ev != "" {
		t.Fatalf("still reloading: %v", ev)
	}
	if ev := read(Secondary, ready, ready, ready); ev != "reload_done/1" {
		t.Fatalf("reload done: %v", ev)
	}
	read(Primary, ready)
	now = now.Add(time.Second)
	if ev := read(Primary, busy); ev != "reload_start/2" {
		t.Fatalf("right list: %v", ev)
	}
}
