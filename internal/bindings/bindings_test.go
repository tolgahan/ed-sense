package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
)

const testPreset = `<?xml version="1.0" encoding="UTF-8" ?>
<Root PresetName="GyroPS" MajorVersion="4" MinorVersion="2">
	<MouseXMode Value="Bindings_MouseYaw" />
	<MouseXDecay Value="0" />
	<MouseYMode Value="Bindings_MousePitch" />
	<MouseYDecay Value="1" />
	<MouseReset>
		<Primary Device="{NoDevice}" Key="" />
		<Secondary Device="Keyboard" Key="Key_F8" />
	</MouseReset>
	<UseBoostJuice>
		<Primary Device="DualShock4" Key="Joy_3" />
		<Secondary Device="Keyboard" Key="Key_Tab" />
	</UseBoostJuice>
	<DeployHeatSink>
		<Primary Device="DualShock4" Key="Joy_POV1Right">
			<Modifier Device="DualShock4" Key="Joy_3" />
		</Primary>
		<Secondary Device="{NoDevice}" Key="" />
	</DeployHeatSink>
	<FireChaffLauncher>
		<Primary Device="DualShock4" Key="Joy_5">
			<Modifier Device="DualShock4" Key="Joy_3" />
		</Primary>
		<Secondary Device="{NoDevice}" Key="" />
	</FireChaffLauncher>
	<UseShieldCell>
		<Primary Device="Keyboard" Key="Key_F9" />
		<Secondary Device="{NoDevice}" Key="" />
	</UseShieldCell>
	<CycleFireGroupNext>
		<Primary Device="DualShock4" Key="Joy_6">
			<Modifier Device="DualShock4" Key="Joy_1" />
		</Primary>
	</CycleFireGroupNext>
	<YawAxisRaw>
		<Binding Device="{NoDevice}" Key="" />
	</YawAxisRaw>
	<RollAxisRaw>
		<Binding Device="DualShock4" Key="Joy_XAxis" />
	</RollAxisRaw>
	<PitchAxisRaw>
		<Binding Device="DualShock4" Key="Joy_YAxis" />
	</PitchAxisRaw>
	<CamYawAxis>
		<Binding Device="DualShock4" Key="Joy_ZAxis" />
	</CamYawAxis>
</Root>`

func TestParse(t *testing.T) {
	b, err := Parse([]byte(testPreset))
	if err != nil {
		t.Fatal(err)
	}
	if b.Preset != "GyroPS" || !b.Has(HeatSink) || !b.Has(Chaff) || !b.Has(ShieldCell) || !b.Has(Boost) {
		t.Fatalf("parse: %+v", b)
	}
	hs := b.actions[HeatSink][0]
	if hs.input.button != dualsense.DpadRight || len(hs.modifiers) != 1 || hs.modifiers[0].button != dualsense.Circle {
		t.Fatalf("heat sink is Circle + Right: %+v", hs)
	}
	if b.actions[ShieldCell][0].input.key != 0x78 {
		t.Fatal("F9 is virtual key 0x78")
	}
	if !b.modifiers[input{button: dualsense.Circle}] || !b.modifiers[input{button: dualsense.Square}] {
		t.Fatal("Circle and Square are modifiers")
	}
	if b.TurnSticks != [4]bool{true, true, false, false} {
		t.Fatalf("turn sticks %v: roll and pitch on the left stick, the camera is not a turn", b.TurnSticks)
	}
	if b.Mouse != (Mouse{Turns: [2]bool{true, true}, Holds: [2]bool{true, false}}) {
		t.Fatalf("mouse %+v: X yaws and holds, Y pitches and decays", b.Mouse)
	}
	if !b.Has(MouseReset) || b.actions[MouseReset][0].input.key != 0x77 {
		t.Fatal("F8 resets the mouse")
	}
	if b, _ := Parse([]byte(`<Root PresetName="X"><MouseXMode Value="" /><MouseXDecay Value="0" /></Root>`)); b.Mouse != (Mouse{}) {
		t.Fatalf("the mouse flies nothing: %+v", b.Mouse)
	}
}

func TestDetector(t *testing.T) {
	b, _ := Parse([]byte(testPreset))
	d := NewDetector(b)
	keys := map[int]bool{}
	step := func(held, pressed dualsense.Button) []Action {
		return d.Update(dualsense.State{OK: true, Buttons: held, Pressed: pressed}, func(vk int) bool { return keys[vk] })
	}
	one := func(got []Action, want Action) bool { return len(got) == 1 && got[0] == want }
	circle, right, l1 := dualsense.Circle, dualsense.DpadRight, dualsense.L1

	// Circle is also a modifier: alone it boosts on release
	if got := step(circle, circle); len(got) != 0 {
		t.Fatalf("boost before release: %v", got)
	}
	if got := step(0, 0); !one(got, Boost) {
		t.Fatalf("boost on release: %v", got)
	}
	// Circle + Right is a heat sink, and letting go of Circle then is no boost
	step(circle, circle)
	if got := step(circle|right, right); !one(got, HeatSink) {
		t.Fatalf("heat sink: %v", got)
	}
	step(circle, 0)
	if got := step(0, 0); len(got) != 0 {
		t.Fatalf("boost after a combo: %v", got)
	}
	step(circle, circle)
	if got := step(circle|l1, l1); !one(got, Chaff) {
		t.Fatalf("chaff: %v", got)
	}
	step(0, 0)
	if got := step(l1, l1); len(got) != 0 {
		t.Fatalf("L1 alone is not watched: %v", got)
	}
	step(0, 0)
	keys[0x78] = true
	if got := step(0, 0); !one(got, ShieldCell) {
		t.Fatalf("F9: %v", got)
	}
	if got := step(0, 0); len(got) != 0 {
		t.Fatal("a held key fires once")
	}
	var none *Detector
	if none.Update(dualsense.State{}, nil) != nil || none.Headlook() {
		t.Fatal("no bindings, no actions")
	}
}

func TestHeadlook(t *testing.T) {
	const preset = `<Root PresetName="GyroPS">
	<MouseHeadlook Value="1" />
	<HeadLookToggle>
		<Primary Device="DualShock4" Key="Joy_11" />
		<Secondary Device="Mouse" Key="Mouse_3" />
		<ToggleOn Value="%s" />
	</HeadLookToggle>
</Root>`
	l3 := dualsense.L3
	for _, toggle := range []bool{false, true} {
		v := "0"
		if toggle {
			v = "1"
		}
		b, err := Parse([]byte(strings.Replace(preset, "%s", v, 1)))
		if err != nil || !b.Mouse.Headlook || len(b.headlook) != 1 || b.headlookToggles != toggle {
			t.Fatalf("parse (toggle %v): %v %+v", toggle, err, b)
		}
		d := NewDetector(b)
		d.SetShipControls(true)
		step := func(held, pressed dualsense.Button) bool {
			if got := d.Update(dualsense.State{OK: true, Buttons: held, Pressed: pressed}, nil); len(got) != 0 {
				t.Fatalf("head look is no fired action: %v", got)
			}
			return d.Headlook()
		}
		if step(0, 0) {
			t.Fatal("off at first")
		}
		if !step(l3, l3) || !step(l3, 0) {
			t.Fatal("on while L3 is held")
		}
		if on := step(0, 0); on != toggle {
			t.Fatalf("L3 let go (toggle %v): %v", toggle, on)
		}
		if toggle && (step(l3, l3) || step(0, 0)) {
			t.Fatal("a second press turns it off")
		}
		if toggle {
			// on foot L3 sprints: no head look toggle
			d.SetShipControls(false)
			step(l3, l3)
			d.SetShipControls(true)
			if step(0, 0) {
				t.Fatal("toggled outside the ship controls")
			}
			step(l3, l3)
			if d.ResetHeadlook(); step(0, 0) {
				t.Fatal("reset")
			}
		}
	}
	b, _ := Parse([]byte(`<Root PresetName="X"><MouseHeadlook Value="0" /><HeadLookToggle><Primary Device="DualShock4" Key="Joy_11" /></HeadLookToggle></Root>`))
	d := NewDetector(b)
	if d.Update(dualsense.State{OK: true, Buttons: dualsense.L3, Pressed: dualsense.L3}, nil); d.Headlook() {
		t.Fatal("mouse headlook off: the mouse keeps flying")
	}
}

func TestShipPreset(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("StartPreset.4.start", "GeneralPS\r\nGyroPS\r\nSRV\r\nFoot\r\n")
	write("GyroPS.4.1.binds", testPreset)
	write("GyroPS.4.2.binds", testPreset)
	write("GyroPS.3.9.binds", testPreset)
	if preset, file := ShipPreset(dir); preset != "GyroPS" || filepath.Base(file) != "GyroPS.4.2.binds" {
		t.Fatalf("preset %q file %q", preset, file)
	}
	w := NewWatcher(dir)
	if !w.Poll(time.Now()) || !w.Bindings().Has(Boost) {
		t.Fatal("watcher did not load the preset")
	}
	if w.Poll(time.Now().Add(10 * time.Second)) {
		t.Fatal("unchanged preset reported as changed")
	}
}
