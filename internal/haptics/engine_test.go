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
	"github.com/tolgahan/ed-sense/internal/hud"
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

// turnFelt: the turn feel's level before the "waves" swell.
func (r *rig) turnFelt() float64 { return r.e.turn.felt }

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
		mods = append(mods, elite.Module{Name: c, Class: c, Size: 2})
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
	large := []elite.Module{{Name: "mc", Class: "multicannon", Size: 3}, {Name: "mc", Class: "multicannon", Size: 3}}
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
	if turnLevel(0.01) != 0 {
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
	if l := r.turnFelt(); l < 0.15 || l > 0.3 {
		t.Fatalf("a slow gyro turn after 1 s is felt but light: %v", l)
	}
	if v := r.synth.layers["maneuver"].v; v.Wave != Sine || v.F0 < 150 || v.F0 > 200 || v.TremDepth != 0 || v.GateHz != 0 {
		t.Fatalf("waves: a smooth sine: %+v", v)
	}
	// a hand turning the controller at an uneven speed: felt all along
	lo, hi := 1.0, 0.0
	for i := range 120 {
		r.run(1, gyro([]float64{6, 34, 12, 28}[i%4]))
		l := r.turnFelt()
		lo, hi = math.Min(lo, l), math.Max(hi, l)
	}
	if lo < 0.15 || hi-lo > 0.05 {
		t.Fatalf("an uneven gyro turn comes and goes: %v to %v", lo, hi)
	}
	if len(r.e.due) != 0 || len(r.synth.shots) != 0 {
		t.Fatal("a steady turn does not kick")
	}
	r.run(100, pad)
	if l := r.turnFelt(); l > 0 {
		t.Fatalf("quiet after the turn: %v", l)
	}
	// rolling turns the mouse sideways too, by the roll mix (DSX's 60%)
	roll := dualsense.State{OK: true, Gyro: [3]int16{0, 0, 547}} // 33 deg/s: 20 sideways
	if r.run(40, roll); r.turnFelt() < 0.15 {
		t.Fatalf("a roll moves the mouse sideways: %v", r.turnFelt())
	}
	r.cfg.GyroRollMix = 0
	if r.run(100, roll); r.turnFelt() > 0 {
		t.Fatal("with roll mix 0, rolling the controller aims nothing")
	}
	r.cfg.GyroRollMix = 0.6
	r.run(6, gyro(400))
	if r.turnFelt() < 0.3 || len(r.synth.shots) == 0 {
		t.Fatalf("a flick is firm and kicks: %v", r.turnFelt())
	}
	r.e.playNow("shield_hit", r.now)
	r.run(2, gyro(400))
	if l := r.turnFelt(); l > 0.2 {
		t.Fatalf("the turn steps back under a hit: %v", l)
	}
	r.run(20, gyro(400))
	if l := r.turnFelt(); l < 0.42 {
		t.Fatalf("and comes back: %v", l)
	}
	touch := gyro(400)
	touch.Touch = true
	r.run(120, touch)
	if l := r.turnFelt(); l > 0 {
		t.Fatalf("a finger on the touchpad pauses the gyro: %v", l)
	}
	stick := dualsense.State{OK: true, Sticks: [4]float64{0.7, 0, 0, 0}}
	r.run(20, stick)
	if l := r.turnFelt(); l < 0.3 {
		t.Fatalf("the left stick turns too: %v", l)
	}
	// the blue zone: no hum and no flick push
	r.g.HUD.BlueZone = hud.Tracked[bool]{Value: true, OK: true, At: r.now}
	r.run(40, stick)
	r.synth.shots = nil
	if r.run(6, gyro(400)); r.turnFelt() != 0 || len(r.synth.shots) != 0 {
		t.Fatalf("felt in the blue zone: %v, %d shots", r.turnFelt(), len(r.synth.shots))
	}
	r.g.HUD.BlueZone = hud.Tracked[bool]{Value: false, OK: true, At: r.now}
	if r.run(40, stick); r.turnFelt() < 0.3 {
		t.Fatal("felt again out of the blue zone")
	}
	r.g.OnStatus(elite.Status{Flags: ship, GuiFocus: 5}, r.now)
	r.run(1, gyro(400))
	if r.turnFelt() > 0 {
		t.Fatal("no turning in menus")
	}
}

// Elite's mouse with decay off: a controller turned and held still keeps
// the ship turning, and the feel; turning back or the mouse reset key ends
// it. With decay on, the feel ends with the movement.
func TestGyroTurnHolds(t *testing.T) {
	r := newRig(true)
	holding := &bindings.Bindings{Mouse: bindings.Mouse{Turns: [2]bool{true, true}, Holds: [2]bool{true, true}}}
	r.e.SetBindings(holding)
	r.status(ship, 0)
	r.run(12, gyro(40)) // 12 degrees in 0.3 s
	r.run(80, pad)      // then held still for 2 s
	if l := r.turnFelt(); l < 0.25 {
		t.Fatalf("a controller held tilted keeps the feel: %v", l)
	}
	r.run(12, gyro(-40))
	r.run(100, pad)
	if l := r.turnFelt(); l > 0 {
		t.Fatalf("turned back: %v", l)
	}
	r.run(12, gyro(40))
	r.run(20, pad)
	r.e.OnActions([]bindings.Action{bindings.MouseReset}, r.g, r.now)
	r.run(100, pad)
	if l := r.turnFelt(); l > 0 {
		t.Fatalf("the mouse reset key centres it: %v", l)
	}
	r.run(12, gyro(40))
	r.g.OnStatus(elite.Status{Flags: ship, GuiFocus: 5}, r.now)
	r.run(40, pad)
	r.g.OnStatus(elite.Status{Flags: ship}, r.now)
	if r.run(20, pad); r.turnFelt() < 0.25 {
		t.Fatal("the deflection holds through a panel")
	}
	r.status(ship|elite.Docked, 0)
	r.run(4, pad)
	r.status(ship, 0)
	if r.run(40, pad); r.turnFelt() > 0 {
		t.Fatal("docking centres it")
	}

	r.e.SetBindings(nil) // decay on
	r.run(12, gyro(40))
	r.run(100, pad)
	if l := r.turnFelt(); l > 0 {
		t.Fatalf("decaying: the feel ends with the movement: %v", l)
	}
	r.e.SetBindings(&bindings.Bindings{}) // the mouse turns nothing
	if r.run(20, gyro(400)); r.turnFelt() > 0 || len(r.synth.shots) != 0 {
		t.Fatal("the gyro turns nothing")
	}
}

// Gyro aim off: turning the controller turns nothing and is not felt; the
// sticks still are.
func TestTurningWithoutGyroAim(t *testing.T) {
	r := newRig(true)
	r.cfg.GyroAim = false
	r.status(ship, 0)
	r.run(40, gyro(400))
	if l := r.turnFelt(); l > 0 || len(r.synth.shots) != 0 {
		t.Fatalf("controller motion felt: %v", l)
	}
	r.run(40, dualsense.State{OK: true, Sticks: [4]float64{0.8, 0, 0, 0}})
	if l := r.turnFelt(); l < 0.3 {
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
	r.g.HUD.Heat = hud.Tracked[int]{Value: 85, OK: true, At: r.now}
	r.run(1, pad)
	if r.e.heat < 0.84 || r.e.heat > 0.86 || r.synth.layers["heat_build"] == nil {
		t.Fatalf("the HUD's heat: %v", r.e.heat)
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

func TestHUDFeels(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	r.g.HUD = hud.State{Shield: hud.Tracked[int]{Value: 20, OK: true, At: r.now}, Splash: 3}
	r.e.OnHUD([]hud.Event{{Kind: hud.ShieldHit, Strength: 0.8, Pan: 1}}, r.g, r.now)
	if len(r.e.due) != 1 || r.e.due[0].Pan != 1 {
		t.Fatal("a shield hit with its pan")
	}
	r.e.Tick(r.now, r.g, pad)
	if len(r.synth.shots) == 0 || r.synth.shots[0].v.L >= r.synth.shots[0].v.R {
		t.Fatal("a hit from the right on the right")
	}
	if r.synth.layers["shield_low"] == nil || r.synth.layers["shield_sizzle"] == nil {
		t.Fatal("low shields crackle, sustained fire sizzles")
	}
	// hits on the target: felt only while firing, on the side fired with
	r.g.Modules = weapons("multicannon")
	r.status(ship|elite.HardpointsDeployed, 0)
	r.due()
	r.e.OnHUD([]hud.Event{{Kind: hud.TargetHit, Strength: 0.5}}, r.g, r.now)
	if len(r.due()) != 0 {
		t.Fatal("a target hit without firing")
	}
	r.run(22, pulled(true, false)) // past the spin-up
	r.e.due = nil
	r.e.OnHUD([]hud.Event{{Kind: hud.TargetHit, Strength: 0.5}}, r.g, r.now)
	if len(r.e.due) != 1 || r.e.due[0].Effect != "target_hit" || r.e.due[0].Side != RightSide {
		t.Fatalf("a target hit firing R2: %+v", r.e.due)
	}
	r.e.due = nil
	r.e.OnHUD([]hud.Event{{Kind: hud.HullHit, Strength: 0.6}}, r.g, r.now)
	if got := r.due(); len(got) != 1 || got[0] != "hull_hit_hud" {
		t.Fatalf("hull hit: %v", got)
	}
	r.g.HUD.Hull = hud.Tracked[int]{Value: 15, OK: true, At: r.now}
	r.run(1, pad)
	if l := r.level("hull_creak"); l < 0.4 {
		t.Fatalf("a weak hull creaks: %v", l)
	}
	// docked: nothing
	r.status(ship|elite.Docked, 0)
	r.due()
	r.e.OnHUD([]hud.Event{{Kind: hud.ShieldHit, Strength: 1}}, r.g, r.now)
	if len(r.e.due) != 0 {
		t.Fatal("no HUD feel docked")
	}
}

func TestBoost(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	caps := func(eng float64) {
		r.g.HUD.Capacitors = hud.Tracked[[3]float64]{Value: [3]float64{1, eng, 1}, OK: true, At: r.now}
	}
	caps(0.1)
	r.e.startBoost(r.now, r.g)
	if got := r.due(); len(got) != 1 || got[0] != "boost_empty" {
		t.Fatalf("ENG 10%%: %v", got)
	}
	caps(0.8)
	r.e.startBoost(r.now, r.g)
	if got := r.due(); len(got) != 1 || got[0] != "boost" {
		t.Fatalf("ENG 80%%: %v", got)
	}
	press := func(eng, after float64) {
		caps(eng)
		r.e.startBoost(r.now, r.g)
		r.now = r.now.Add(900 * time.Millisecond)
		caps(after)
		r.e.learnBoost(r.now, r.g)
	}
	press(0.3, 0.3) // no boost at 30%
	if r.e.boost.need < 0.3 {
		t.Fatalf("need %v after a failed boost at 30%%", r.e.boost.need)
	}
	press(0.2, 0.05) // ... but this ship boosts at 20%
	if r.e.boost.need > 0.2 {
		t.Fatalf("need %v after a boost at 20%%", r.e.boost.need)
	}
}

func TestReloadFeels(t *testing.T) {
	r := newRig(true)
	r.status(ship|elite.HardpointsDeployed, 0)
	r.e.OnHUD([]hud.Event{{Kind: hud.ReloadStart, Strength: 1, Side: hud.LeftSide}}, r.g, r.now)
	if len(r.e.due) != 1 || r.e.due[0].Effect != "reload" || r.e.due[0].Side != LeftSide {
		t.Fatalf("reload start: %+v", r.e.due)
	}
	r.e.due = nil
	r.e.OnHUD([]hud.Event{{Kind: hud.ReloadDone, Strength: 1, Side: hud.LeftSide}}, r.g, r.now)
	if got := r.due(); len(got) != 1 || got[0] != "reload_done" {
		t.Fatalf("reload done: %v", got)
	}
	// L2 fires 3 multi-cannons, all reloading; R2 2 beams
	key := hud.FireKey(0, true)
	r.g.FireLists = map[int][2]hud.FireList{key: {
		hud.Secondary: {Known: true, Key: key, Names: []string{"MULTI-CANNON"}, Counts: []int{3}, Classes: []string{"multicannon"}},
		hud.Primary:   {Known: true, Key: key, Names: []string{"BEAM LASER"}, Counts: []int{2}, Classes: []string{"beam"}, Energy: 2},
	}}
	r.g.HUD.Lists[hud.Secondary] = hud.FireList{At: r.now.Add(25 * time.Millisecond), Ammo: 3, Reloading: 3}
	r.run(1, pulled(true, true))
	if r.level("fire_secondary") > 0 {
		t.Fatal("reloading multi-cannons are not felt")
	}
	if l := r.synth.layers["fire_primary"]; l == nil || l.target <= 0 || l.v.F0 != WeaponTexture("beam").F0 {
		t.Fatalf("the beams on R2: %+v", l)
	}
}

// A fire group with only a heat sink on L2: a press feels like a heat sink
// launch, once.
func TestUtilityFireGroup(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	key := hud.FireKey(0, false)
	r.g.FireLists = map[int][2]hud.FireList{key: {
		hud.Secondary: {Known: true, Names: []string{"HEATSINK"}, Counts: []int{1}, Classes: []string{"heatsink"}, Utility: true},
		hud.Primary:   {Known: true, Names: []string{"KILL WARRANT SCANNER"}, Counts: []int{1}, Classes: []string{"scanner"}, Utility: true},
	}}
	played := map[string]int{}
	r.e.onPlay = func(effect string) { played[effect]++ }
	r.run(10, pulled(false, true)) // held for 250 ms
	if n := played["heat_sink"]; n != 1 {
		t.Fatalf("heat sink launches: %d", n)
	}
	played = map[string]int{}
	r.run(1, pad)
	r.run(1, pulled(false, true))
	if played["heat_sink"] != 0 {
		t.Fatal("a second press within 2 s is not another launch")
	}
	r.run(1, pulled(true, false))
	if r.level("scanner") <= 0 {
		t.Fatal("the scanner hums while held")
	}
}

// Into the hyperspace tunnel: a short, soft swell, then quiet for the rest
// of the jump. Elite sets the jump and charging flags from the start of the
// countdown; the tunnel starts 5 s after StartJump, and the drive charging
// is felt until then.
func TestHyperspaceSwell(t *testing.T) {
	r := newRig(true)
	r.status(ship|elite.Supercruise, 0)
	r.g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, r.now)
	r.status(ship|elite.Supercruise|elite.FSDCharging|elite.FSDJump, 0)
	if r.run(190, pad); r.level("hyperspace") > 0 || r.level("fsd_charge") > 0 {
		t.Fatal("counting down: nothing felt")
	}
	low, peak := 1000.0, 0.0
	for range 50 {
		r.run(1, pad)
		peak = math.Max(peak, r.level("hyperspace"))
		if l := r.synth.layers["hyperspace"]; l != nil && l.target > 0 {
			if l.v.Wave != Sine {
				t.Fatalf("a smooth sine: %+v", l.v)
			}
			low = math.Min(low, l.v.F0)
		}
	}
	if peak < 0.9 || low < 150 {
		t.Fatalf("the swell: peak %v, lowest %v Hz", peak, low)
	}
	if r.level("fsd_charge") > 0 {
		t.Fatal("no charging feel in the tunnel")
	}
	if r.run(90, pad); r.level("hyperspace") > 0 {
		t.Fatal("quiet after 3 s")
	}
}

// In the hyperspace tunnel the ship does not turn: a held virtual mouse
// stick is not felt there, and is again after the arrival.
func TestNoTurnInTheTunnel(t *testing.T) {
	r := newRig(true)
	r.e.SetBindings(&bindings.Bindings{Mouse: bindings.Mouse{Turns: [2]bool{true, true}, Holds: [2]bool{true, true}}})
	r.status(ship|elite.Supercruise, 0)
	r.run(12, gyro(40))
	r.run(20, pad)
	if r.turnFelt() == 0 {
		t.Fatal("a held turn")
	}
	r.g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, r.now)
	r.status(ship|elite.Supercruise|elite.FSDCharging|elite.FSDJump, 0)
	r.run(210, pad)
	if l := r.turnFelt(); l > 0 {
		t.Fatalf("turn felt in the tunnel: %v", l)
	}
	r.g.OnEvent(elite.Event{"event": "FSDJump"}, true, r.now)
	r.status(ship|elite.Supercruise, 0)
	if r.run(20, pad); r.turnFelt() == 0 {
		t.Fatal("felt again after the arrival")
	}
}

// Mouse headlook: turning the controller moves the view, and the ship's
// virtual stick stays where it was.
func TestHeadlookMovesTheView(t *testing.T) {
	r := newRig(true)
	r.e.SetBindings(&bindings.Bindings{Mouse: bindings.Mouse{Turns: [2]bool{true, true}, Holds: [2]bool{true, true}, Headlook: true}})
	r.status(ship, 0)
	r.e.SetHeadlook(true)
	r.run(20, gyro(40))
	r.run(20, gyro(-80))
	r.e.SetHeadlook(false)
	if r.run(40, pad); r.turnFelt() > 0 || r.e.turn.mouse != [2]float64{} {
		t.Fatalf("looking around turned the ship: %v %v", r.turnFelt(), r.e.turn.mouse)
	}
}

// A jump from normal space: nothing thumps when the jump flag comes on at
// the start of the countdown. jump_feel "calm" pulses once a second while
// the drive charges; "off" feels nothing, not even the swell.
func TestJumpFeels(t *testing.T) {
	r := newRig(true)
	r.cfg.JumpFeel = config.JumpCalm
	r.status(ship, 0)
	r.status(ship|elite.FSDCharging, 0)
	r.g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, r.now)
	r.status(ship|elite.FSDCharging|elite.FSDJump, 0)
	if due := r.due(); len(due) != 0 {
		t.Fatalf("played at the countdown: %v", due)
	}
	lo, hi := 1.0, 0.0
	for range 40 {
		r.run(1, pad)
		l := r.level("fsd_charge")
		lo, hi = math.Min(lo, l), math.Max(hi, l)
	}
	if lo > 0.05 || hi < 0.9 {
		t.Fatalf("calm: a pulse a second while charging: %v to %v", lo, hi)
	}
	if v := r.synth.layers["fsd_charge"].v; v.F0 < 150 || v.TremHz != 0 {
		t.Fatalf("a smooth tone: %+v", v)
	}

	r = newRig(true)
	r.cfg.JumpFeel = config.JumpOff
	r.status(ship|elite.Supercruise, 0)
	r.g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, r.now)
	r.status(ship|elite.Supercruise|elite.FSDCharging|elite.FSDJump, 0)
	for range 300 {
		if r.run(1, pad); r.level("fsd_charge") > 0 || r.level("hyperspace") > 0 {
			t.Fatal("off: nothing felt")
		}
	}
}

// turn_feel "push": a turn is felt as it starts and as it ends, and a steady
// turn goes quiet. "off": no turn feel and no flick push.
func TestTurnFeels(t *testing.T) {
	r := newRig(true)
	r.cfg.TurnFeel = config.TurnPush
	r.status(ship, 0)
	stick := dualsense.State{OK: true, Sticks: [4]float64{0, -0.8, 0, 0}}
	r.run(12, stick)
	if l := r.level("maneuver"); l < 0.25 {
		t.Fatalf("push: the turn starts: %v", l)
	}
	if v := r.synth.layers["maneuver"].v; v.F0 < 150 || v.TremHz != 0 {
		t.Fatalf("push: a smooth tone: %+v", v)
	}
	r.run(200, stick)
	if l := r.level("maneuver"); l > 0 {
		t.Fatalf("push: a steady turn is quiet: %v", l)
	}
	r.run(12, pad)
	if l := r.level("maneuver"); l < 0.2 {
		t.Fatalf("push: the turn ends: %v", l)
	}
	r.run(200, pad)
	if l := r.level("maneuver"); l > 0 {
		t.Fatalf("push: quiet after: %v", l)
	}
	// a quick reversal is felt like a start
	r.run(200, stick)
	r.run(1, pad)
	r.run(12, dualsense.State{OK: true, Sticks: [4]float64{0, 0.8, 0, 0}})
	if l := r.level("maneuver"); l < 0.25 {
		t.Fatalf("push: a reversal: %v", l)
	}

	r = newRig(true)
	r.cfg.TurnFeel = config.TurnOff
	r.status(ship, 0)
	r.run(40, stick)
	if r.run(6, gyro(400)); r.level("maneuver") > 0 || len(r.synth.shots) != 0 || len(r.e.due) != 0 {
		t.Fatal("off: nothing felt")
	}
}

// turn_feel "waves": the level swells about every 2.2 s while a turn holds,
// and a turn that starts again soon after the last one ended starts on a
// crest.
func TestWavesSwell(t *testing.T) {
	r := newRig(true)
	r.status(ship, 0)
	stick := dualsense.State{OK: true, Sticks: [4]float64{0, -1, 0, 0}}
	r.run(20, stick)
	lo, hi := 1.0, 0.0
	for range 100 {
		r.run(1, stick)
		l := r.level("maneuver")
		lo, hi = math.Min(lo, l), math.Max(hi, l)
	}
	if hi < 0.4 || lo > 0.1*hi {
		t.Fatalf("a held turn swells: %v to %v", lo, hi)
	}
	for gap := 4; gap <= 36; gap += 8 {
		r.run(20, pad) // let go, and push again after gap ticks more
		r.run(gap, pad)
		r.run(4, stick)
		if l, full := r.level("maneuver"), r.turnFelt(); l < 0.5*full || full == 0 {
			t.Fatalf("a turn %d ms after the last one: %v of %v", (20+gap)*25, l, full)
		}
	}
}
