package haptics

import (
	"math"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/bindings"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

var testStart = time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)

const ship = elite.InMainShip | elite.ShieldsUp

type rig struct {
	cfg   *config.Config
	g     *game.State
	synth *Synth
	e     *Engine
	now   time.Time
}

// newRig: an engine with native haptics, or rumble if synth is false.
func newRig(synth bool) *rig {
	cfg := config.Default()
	r := &rig{cfg: &cfg, g: game.New(), e: New(&cfg), now: testStart}
	if synth {
		r.synth = NewSynth()
		r.e.UseSynth(r.synth, true)
	}
	return r
}

func (r *rig) status(flags elite.Flags, flags2 elite.Flags2) {
	s := elite.Status{Flags: flags, Flags2: flags2}
	if r.g.HaveStatus {
		r.e.OnStatus(r.g.Status, s, r.now)
	}
	r.g.OnStatus(s, r.now)
}

// run ticks n times, 25 ms apart.
func (r *rig) run(n int, pad dualsense.State) (left, right float64) {
	for range n {
		r.now = r.now.Add(25 * time.Millisecond)
		left, right = r.e.Tick(r.now, r.g, pad)
	}
	return left, right
}

func (r *rig) level(layer string) float64 {
	if l := r.synth.layers[layer]; l != nil {
		return l.target
	}
	return 0
}

// due returns the one-shots waiting to play and forgets them.
func (r *rig) due() []string {
	var names []string
	for _, s := range r.e.due {
		names = append(names, s.Effect)
	}
	r.e.due = nil
	return names
}

func weapons(classes ...string) []elite.Module {
	var mods []elite.Module
	for _, c := range classes {
		mods = append(mods, elite.Module{Class: c, Size: 2})
	}
	return mods
}

var pad = dualsense.State{OK: true}

func pulled(r2, l2 bool) dualsense.State {
	p := pad
	if r2 {
		p.R2 = 255
	}
	if l2 {
		p.L2 = 255
	}
	return p
}

func TestRumbleFiring(t *testing.T) {
	r := newRig(false)
	r.status(ship, 0)
	if l, rr := r.e.Tick(r.now, r.g, dualsense.State{OK: true, R2: 255, Buttons: dualsense.R2}); l != 0 || rr != 0 {
		t.Fatalf("hardpoints in: silent, got %v %v", l, rr)
	}
	r.status(ship|elite.HardpointsDeployed, 0)
	if l, rr := r.e.Tick(r.now, r.g, pad); l < 0.3 || rr < 0.3 {
		t.Fatalf("hardpoints clack: %v %v", l, rr)
	}
	later := r.now.Add(time.Second)
	if l, rr := r.e.Tick(later, r.g, pulled(true, false)); l != 0 || rr < 0.15 {
		t.Fatalf("R2 on the right: %v %v", l, rr)
	}
	if l, rr := r.e.Tick(later, r.g, pulled(false, true)); rr != 0 || l < 0.15 {
		t.Fatalf("L2 on the left: %v %v", l, rr)
	}
	r.g.Status.GuiFocus = 6
	if l, rr := r.e.Tick(later, r.g, pulled(true, false)); l != 0 || rr != 0 {
		t.Fatalf("no firing in the galaxy map: %v %v", l, rr)
	}
	r.g.Status.GuiFocus = 0
	r.e.Tick(later, r.g, dualsense.State{OK: true, Pressed: dualsense.Circle, Buttons: dualsense.Circle})
	if l, rr := r.e.Tick(later.Add(100*time.Millisecond), r.g, pad); l < 0.5 || rr < 0.5 {
		t.Fatalf("Circle boosts: %v %v", l, rr)
	}
	if l, rr := r.e.Tick(later.Add(5*time.Second), r.g, pad); l != 0 || rr != 0 {
		t.Fatalf("the pulses end: %v %v", l, rr)
	}
	r.cfg.HapticsStrength = 0
	r.e.playNow("boost", later)
	if l, rr := r.e.Tick(later, r.g, pad); l != 0 || rr != 0 {
		t.Fatal("strength 0 mutes")
	}
}

func TestRumbleEvents(t *testing.T) {
	r := newRig(false)
	r.status(ship, 0)
	r.e.OnEvent(elite.Event{"event": "ShieldState", "ShieldsUp": false}, r.g, r.now)
	if l, rr := r.e.Tick(r.now, r.g, dualsense.State{}); l < 0.9 || rr < 0.9 {
		t.Fatalf("shields down is heavy: %v %v", l, rr)
	}
	r.e.pulses = nil
	r.e.OnEvent(elite.Event{"event": "HullDamage", "Fighter": true}, r.g, r.now)
	if l, _ := r.e.Tick(r.now, r.g, dualsense.State{}); l != 0 {
		t.Fatal("the fighter's hull damage")
	}
	r.e.OnStatus(elite.Status{Flags: elite.InMainShip, Pips: []int{4, 4, 4}}, elite.Status{Flags: elite.InMainShip, Pips: []int{6, 2, 4}}, r.now)
	if _, rr := r.e.Tick(r.now, r.g, dualsense.State{}); rr == 0 {
		t.Fatal("pips tick on the right")
	}
}

func TestNativeFiring(t *testing.T) {
	r := newRig(true)
	r.g.Modules = weapons("beam", "multicannon")
	r.status(ship|elite.HardpointsDeployed, 0)
	if l, rr := r.run(1, pulled(true, false)); l != 0 || rr != 0 {
		t.Fatal("native haptics don't drive the motors")
	}
	if l := r.synth.layers["fire_primary"]; l == nil || l.v.R != 1 || l.v.L != 0 || l.v.F0 != 110 {
		t.Fatalf("R2 beams fire at once: %+v", l)
	}
	r.run(1, pulled(false, true))
	if r.level("fire_primary") != 0 {
		t.Fatal("R2 released: the layer fades")
	}
	if r.synth.layers["fire_secondary"] != nil {
		t.Fatal("L2 multi-cannons: no fire while they spin up")
	}
	if l := r.synth.layers["fire_secondary_spin"]; l == nil || l.v.L != 1 || l.v.R != 0 {
		t.Fatalf("L2 multi-cannons whir on the left: %+v", l)
	}
	r.run(21, pulled(false, true))
	if l := r.synth.layers["fire_secondary"]; l == nil || l.v.F0 != 55 || r.level("fire_secondary") == 0 {
		t.Fatalf("medium multi-cannons fire after 0.5 s: %+v", l)
	}
	if r.level("fire_secondary_spin") != 0 {
		t.Fatal("the whir stops when they fire")
	}
	r.status(ship, 0)
	r.run(2, pad)
	if len(r.synth.shots) == 0 {
		t.Fatal("the hardpoints clack")
	}
	// a railgun charges while held and cracks on release, on the right
	r.status(ship|elite.HardpointsDeployed, 0)
	r.g.Modules = weapons("railgun")
	r.run(1, pad)
	r.synth.shots = nil
	r.run(24, pulled(true, false))
	if l := r.synth.layers["fire_primary"]; l == nil || l.v.F0 < 100 {
		t.Fatal("railgun charge")
	}
	r.run(1, pad)
	if len(r.synth.shots) == 0 || r.synth.shots[0].v.R != 1 || r.synth.shots[0].v.L != 0 {
		t.Fatal("the railgun cracks on the right")
	}
}

func TestMultiCannonSpinUp(t *testing.T) {
	large := []elite.Module{{Class: "multicannon", Size: 3}, {Class: "multicannon", Size: 3}}
	r := newRig(true)
	r.g.Modules = large
	r.status(ship|elite.HardpointsDeployed, 0)
	fires := func(n int) bool {
		r.run(n, pulled(true, false))
		return r.level("fire_primary") > 0
	}
	if fires(50) {
		t.Fatal("large multi-cannons still spin up at 1.25 s")
	}
	if !fires(12) {
		t.Fatal("large multi-cannons fire after 1.5 s")
	}
	// released for 0.3 s, they spin down a fifth and fire again 0.3 s later
	r.run(12, pad)
	if fires(8) {
		t.Fatal("spinning up again")
	}
	if !fires(6) {
		t.Fatal("a quick second press fires sooner")
	}
	r.cfg.SpinUpMs["large"] = 0
	r.run(1, pad)
	if !fires(1) {
		t.Fatal("spin_up_ms 0: at once")
	}
}

func TestOnFootShots(t *testing.T) {
	for weapon, want := range map[string]string{
		"$wpn_kinetic_assaultrifle_name;": "shot_kinetic",
		"Manticore Executioner":           "shot_plasma",
		"TK Aphelion":                     "shot_laser",
		"$humanoid_fists_name;":           "",
	} {
		if got := onFootShot(weapon); got != want {
			t.Errorf("%s: %q, want %q", weapon, got, want)
		}
	}
	r := newRig(true)
	r.g.OnStatus(elite.Status{Flags2: elite.OnFoot | elite.OnFootOnPlanet, SelectedWeapon: "Manticore Executioner"}, r.now)
	r.run(1, dualsense.State{OK: true, R2: 255, Pressed: dualsense.R2})
	if len(r.synth.shots) == 0 || r.synth.shots[0].v.L != 0 {
		t.Fatal("a shot on the right")
	}
}

func TestTurnCurve(t *testing.T) {
	if turnLevel(0.01) != 0 || gyroTurn(2) != 0 {
		t.Fatal("no turn is silent")
	}
	prev := 0.0
	for _, x := range []float64{0.06, 0.1, 0.25, 0.5, 0.75, 1} {
		l := turnLevel(x)
		if l <= prev {
			t.Fatalf("not rising at %v: %v <= %v", x, l, prev)
		}
		prev = l
	}
	if l := turnLevel(gyroTurn(20)); l < 0.15 || l > 0.3 {
		t.Fatalf("a slow gyro turn is felt but light: %v", l)
	}
	if l := turnLevel(1); l < 0.44 || l > 0.46 {
		t.Fatalf("full turn: %v", l)
	}
	if v := stickTurn(dualsense.State{Sticks: [4]float64{0.05, -0.08, 1, 1}}, defaultTurnSticks); v != 0 {
		t.Fatalf("a centred left stick is no turn (the right one is the camera): %v", v)
	}
	if v := stickTurn(dualsense.State{Sticks: [4]float64{0, -1, 0, 0}}, defaultTurnSticks); v < 0.99 {
		t.Fatalf("full pitch: %v", v)
	}
}

func gyro(degPerSec float64) dualsense.State {
	return dualsense.State{OK: true, Gyro: [3]int16{0, int16(degPerSec * 16.4), 0}}
}

func TestTurning(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	r.run(40, gyro(20))
	if l := r.level("maneuver"); l < 0.15 {
		t.Fatalf("a slow gyro turn after 1 s: %v", l)
	}
	if v := r.synth.layers["maneuver"].v; v.Wave != Sine || v.F0 < 180 || v.TremDepth != 0 || v.GateHz != 0 {
		t.Fatalf("the turn is one high, smooth sine: %+v", v)
	}
	// a hand turning the controller at an uneven speed: felt all along
	lo, hi := 1.0, 0.0
	for i := range 120 {
		r.run(1, gyro([]float64{6, 34, 12, 28}[i%4]))
		l := r.level("maneuver")
		lo, hi = math.Min(lo, l), math.Max(hi, l)
	}
	if lo < 0.15 || hi-lo > 0.05 {
		t.Fatalf("an uneven gyro turn comes and goes: %v to %v", lo, hi)
	}
	if len(r.e.due) != 0 || len(r.synth.shots) != 0 {
		t.Fatal("a steady turn does not kick")
	}
	r.run(40, pad)
	if l := r.level("maneuver"); l > 0 {
		t.Fatalf("quiet after the turn: %v", l)
	}
	roll := dualsense.State{OK: true, Gyro: [3]int16{0, 0, 200 * 16}}
	if r.run(20, roll); r.level("maneuver") > 0 {
		t.Fatal("rolling the controller aims nothing")
	}
	r.run(6, gyro(400))
	if r.level("maneuver") < 0.3 || len(r.synth.shots) == 0 {
		t.Fatalf("a flick is firm and kicks: %v", r.level("maneuver"))
	}
	r.e.playNow("hull_hit", r.now)
	r.run(2, gyro(400))
	if l := r.level("maneuver"); l > 0.2 {
		t.Fatalf("the turn steps back under a hit: %v", l)
	}
	r.run(20, gyro(400))
	if l := r.level("maneuver"); l < 0.42 {
		t.Fatalf("and comes back: %v", l)
	}
	touch := gyro(400)
	touch.Touch = true
	r.run(70, touch)
	if l := r.level("maneuver"); l > 0 {
		t.Fatalf("a finger on the touchpad pauses the gyro: %v", l)
	}
	stick := dualsense.State{OK: true, Sticks: [4]float64{0.7, 0, 0, 0}}
	r.run(20, stick)
	if l := r.level("maneuver"); l < 0.3 {
		t.Fatalf("the left stick turns too: %v", l)
	}
	r.g.OnStatus(elite.Status{Flags: ship, GuiFocus: 5}, r.now)
	r.run(1, gyro(400))
	if r.level("maneuver") > 0 {
		t.Fatal("no turning in menus")
	}
}

// Gyro aim off: turning the controller turns nothing and is not felt; the
// sticks still are.
func TestTurningWithoutGyroAim(t *testing.T) {
	r := newRig(true)
	r.cfg.GyroAim = false
	r.status(ship, 0)
	r.run(40, gyro(400))
	if l := r.level("maneuver"); l > 0 || len(r.synth.shots) != 0 {
		t.Fatalf("controller motion felt: %v", l)
	}
	r.run(40, dualsense.State{OK: true, Sticks: [4]float64{0.8, 0, 0, 0}})
	if l := r.level("maneuver"); l < 0.3 {
		t.Fatalf("the stick turn is felt: %v", l)
	}
}

func TestHeatEstimate(t *testing.T) {
	r := newRig(true)
	r.g.Modules = weapons("beam", "beam")
	r.status(ship|elite.HardpointsDeployed, 0)
	r.run(400, pulled(true, true)) // 10 s of beams on both triggers
	if r.e.heat < 0.6 || r.e.heat > 0.97 {
		t.Fatalf("heat after 10 s of beams: %v", r.e.heat)
	}
	if r.level("heat_build") <= 0.12 {
		t.Fatal("heat build")
	}
	r.status(ship|elite.HardpointsDeployed|elite.Overheating, 0)
	r.run(1, pad)
	if r.e.heat < 1 || r.synth.layers["overheat"] == nil {
		t.Fatalf("overheating: %v", r.e.heat)
	}
	r.status(ship, 0)
	r.run(800, pad)
	if r.e.heat > 0.1 || r.level("heat_build") > 0 {
		t.Fatalf("cooled down: %v", r.e.heat)
	}
}

// No one-shots in danger; a scan is one sweep from left to right.
func TestDangerIsQuietAndScansSweep(t *testing.T) {
	r := newRig(true)
	r.status(ship|elite.InDanger, 0)
	shots := func(d time.Duration) (n int, voices []Voice) {
		for end := r.now.Add(d); r.now.Before(end); {
			r.synth.shots = nil
			r.run(1, pad)
			if len(r.synth.shots) > 0 {
				n++
				for _, s := range r.synth.shots {
					voices = append(voices, s.v)
				}
			}
		}
		return n, voices
	}
	if n, _ := shots(5 * time.Second); n != 0 {
		t.Fatalf("in danger: %d one-shots in 5 s", n)
	}
	r.status(elite.InMainShip|elite.InDanger, 0) // shields collapsed
	if n, _ := shots(3 * time.Second); n != 0 {
		t.Fatalf("shields down: %d one-shots", n)
	}
	if r.level("shields_offline") <= 0 {
		t.Fatal("the shields-offline rattle")
	}
	for _, scan := range []string{"Cargo", "Crime", "Cabin"} { // they come together
		r.e.OnEvent(elite.Event{"event": "Scanned", "ScanType": scan}, r.g, r.now)
	}
	n, voices := shots(2 * time.Second)
	if n != 1 {
		t.Fatalf("scan: %d one-shots", n)
	}
	var first, last *Voice
	for i := range voices {
		v := &voices[i]
		if v.L+v.R == 0 || v.L > 0 && v.R > 0 {
			continue
		}
		if first == nil || v.Delay < first.Delay {
			first = v
		}
		if last == nil || v.Delay > last.Delay {
			last = v
		}
	}
	if first == nil || first.R != 0 || last.L != 0 {
		t.Fatalf("not left to right: %+v %+v", first, last)
	}
}

func TestStatusFeels(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	r.due()
	for _, c := range []struct {
		flags  elite.Flags
		flags2 elite.Flags2
		want   string
	}{
		{ship | elite.FlightAssistOff, 0, "fa_off"},
		{ship, 0, "fa_on"},
		{ship | elite.FSDCooldown, 0, ""},
		{ship, 0, "fsd_ready"},
		{ship | elite.MassLocked, 0, "mass_lock"},
		{ship, 0, "mass_unlock"},
		{ship, elite.GlideMode, "glide_start"},
		{ship, 0, "glide_end"},
	} {
		r.status(c.flags, c.flags2)
		got := r.due()
		if c.want == "" && len(got) != 0 || c.want != "" && (len(got) != 1 || got[0] != c.want) {
			t.Errorf("%x/%x: %v, want %q", c.flags, c.flags2, got, c.want)
		}
	}
	// dropping from supercruise into a mass lock: no separate thump
	r.status(ship|elite.Supercruise, 0)
	r.due()
	r.status(ship|elite.MassLocked, 0)
	if got := r.due(); len(got) != 0 {
		t.Errorf("supercruise drop: %v", got)
	}
}

func TestJournalFeels(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	for _, c := range []struct {
		ev   elite.Event
		want string
	}{
		{elite.Event{"event": "CollectCargo", "Type": "gold"}, "cargo_collect"},
		{elite.Event{"event": "EjectCargo", "Type": "gold", "Count": 1.0}, "cargo_eject"},
		{elite.Event{"event": "ReceiveText", "From": "CMDR Foo", "Channel": "player", "Message": "o7"}, "message"},
		{elite.Event{"event": "ReceiveText", "From": "Wingman", "Channel": "wing", "Message": "hi"}, "message"},
		{elite.Event{"event": "ReceiveText", "From": "$npc_name_decorate:#name=Pirate;", "Channel": "npc", "Message": "$Pirate_OnStartScanCargo07;"}, ""},
		{elite.Event{"event": "ReceiveText", "From": "", "Channel": "npc", "Message": "x"}, ""},
		{elite.Event{"event": "SystemsShutdown"}, "systems_shutdown"},
		{elite.Event{"event": "ShipTargeted", "TargetLocked": true}, "target_locked"},
		{elite.Event{"event": "ShipTargeted", "TargetLocked": true, "ScanStage": 1.0}, ""},
	} {
		r.e.OnEvent(c.ev, r.g, r.now)
		got := r.due()
		if c.want == "" && len(got) != 0 || c.want != "" && (len(got) != 1 || got[0] != c.want) {
			t.Errorf("%v: %v, want %q", c.ev["event"], got, c.want)
		}
	}
}

func TestShutdownAndReboot(t *testing.T) {
	r := newRig(true)
	r.g.Modules = weapons("multicannon")
	r.status(ship|elite.HardpointsDeployed, 0)
	r.g.OnEvent(elite.Event{"event": "SystemsShutdown"}, true, r.now)
	r.now = r.now.Add(time.Second)
	r.e.Tick(r.now, r.g, dualsense.State{OK: true, R2: 255, Buttons: dualsense.R1})
	if len(r.synth.layers) != 0 {
		t.Fatalf("a dead ship: no weapons, no thrust, got %d layers", len(r.synth.layers))
	}
	r.g.StartShutdown(r.now, -500*time.Millisecond) // rebooting since half a second
	r.e.Tick(r.now, r.g, pad)
	if len(r.synth.shots) == 0 {
		t.Fatal("the reboot feel")
	}
}

func TestPlanetFeels(t *testing.T) {
	r := newRig(true)
	flags := ship | elite.HasLatLong
	alt := 2000.0
	for range 10 { // falling at 100 m/s
		a := alt
		r.g.OnStatus(elite.Status{Flags: flags, Altitude: &a}, r.now)
		r.e.Tick(r.now, r.g, pad)
		alt -= 50
		r.now = r.now.Add(500 * time.Millisecond)
	}
	if r.level("ground_rush") <= 0.3 {
		t.Fatal("the ground rushing up")
	}
	high := 9000.0
	r.g.OnStatus(elite.Status{Flags: flags | elite.AltitudeFromAverageRadius, Altitude: &high}, r.now)
	r.e.Tick(r.now, r.g, pad)
	if r.level("ground_rush") > 0 {
		t.Fatal("nothing far up")
	}
	r.g.OnStatus(elite.Status{Flags: flags, Flags2: elite.GlideMode, Altitude: &high}, r.now)
	r.e.Tick(r.now, r.g, pad)
	if r.synth.layers["glide"] == nil || r.synth.layers["glide_low"] == nil {
		t.Fatal("the glide")
	}
}

func TestThargoidsAndActions(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	r.g.OnEvent(elite.Event{"event": "Music", "MusicTrack": "Combat_Unknown"}, true, r.now)
	r.e.Tick(r.now, r.g, pad)
	if l := r.synth.layers["thargoid"]; l == nil || l.v.TremHz != 0.9 {
		t.Fatal("the Thargoid combat throb")
	}
	r.e.heat = 0.9
	r.e.OnActions([]bindings.Action{bindings.HeatSink}, r.g, r.now)
	if got := r.due(); r.e.heat > 0.1 || len(got) != 1 || got[0] != "heat_sink" {
		t.Fatalf("heat sink: heat %v, %v", r.e.heat, got)
	}
	r.g.OnStatus(elite.Status{Flags: ship, GuiFocus: 5}, r.now)
	r.e.OnActions([]bindings.Action{bindings.Chaff}, r.g, r.now)
	if len(r.due()) != 0 {
		t.Fatal("no action feel in menus")
	}
	// bound boost: Circle alone no longer boosts
	r.e.SetBindings(boostBound())
	r.g.OnStatus(elite.Status{Flags: ship}, r.now)
	r.e.Tick(r.now, r.g, dualsense.State{OK: true, Buttons: dualsense.Circle, Pressed: dualsense.Circle})
	for _, name := range r.due() {
		if name == "boost" {
			t.Fatal("the boost comes from the bindings")
		}
	}
}

func boostBound() *bindings.Bindings {
	b, err := bindings.Parse([]byte(`<Root PresetName="Test"><UseBoostJuice><Primary Device="DualShock4" Key="Joy_3" /></UseBoostJuice></Root>`))
	if err != nil {
		panic(err)
	}
	return b
}
