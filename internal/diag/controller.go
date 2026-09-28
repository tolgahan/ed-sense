package diag

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/haptics"
)

// wait sleeps for d; false if done was closed meanwhile.
func wait(done <-chan struct{}, d time.Duration) bool {
	select {
	case <-done:
		return false
	case <-time.After(d):
		return true
	}
}

// Rumble finds DSX's virtual DualSense, rumbles the left and then the right
// side, and shows the buttons, triggers, gyro and touchpad.
func Rumble(done <-chan struct{}) {
	devices := dualsense.ListHID()
	fmt.Printf("Sony HID interfaces: %d\n", len(devices))
	for _, d := range devices {
		fmt.Printf("  %s\n", d)
		for _, p := range d.Parents {
			fmt.Printf("      <- %s\n", p)
		}
	}
	pad := dualsense.NewLink()
	defer pad.Close()
	pad.Maintain()
	if !pad.Available() {
		fmt.Println("\nNo virtual DualSense found. In DSX, set the controller to DualSense emulation.")
		return
	}
	fmt.Println()
	for _, side := range []struct {
		name        string
		left, right uint8
	}{{"LEFT", 200, 0}, {"RIGHT", 0, 200}} {
		log.Printf("Rumble: %s side (2 s)", side.name)
		pad.SetRumble(side.left, side.right)
		if !wait(done, 2*time.Second) {
			return
		}
		pad.SetRumble(0, 0)
		if !wait(done, time.Second) {
			return
		}
	}
	log.Print("Input: press R2, L2, R1 and Circle, turn the controller, touch the touchpad (15 s)")
	_ = pad.State()
	last := ""
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); {
		st := pad.State()
		line := "no input reports yet"
		if st.OK {
			line = fmt.Sprintf("L2 %3d  R2 %3d  R1 %v  Circle %v  gyro %3.0f deg/s  touch %v",
				st.L2/16*16, st.R2/16*16, st.Held(dualsense.R1), st.Held(dualsense.Circle), math.Round(st.AimDegPerSec()/20)*20, st.Touch)
		}
		if line != last {
			log.Print(line)
			last = line
		}
		if !wait(done, 50*time.Millisecond) {
			return
		}
	}
	log.Print("Controller test done.")
}

// Haptics lists the audio outputs, opens the virtual DualSense's and plays
// native haptic effects on each side.
func Haptics(done <-chan struct{}) {
	devices := dualsense.ListAudio()
	fmt.Printf("Audio outputs: %d\n", len(devices))
	for _, d := range devices {
		fmt.Printf("  %s\n", d)
	}
	synth := haptics.NewSynth()
	out := dualsense.NewHapticsOut(synth.Render)
	defer out.Close()
	out.Maintain()
	for i := 0; i < 40 && !out.Active(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !out.Active() {
		fmt.Println("\nNative haptics could not start (see the log line above).")
		return
	}
	fmt.Println()
	for _, s := range hapticsSteps() {
		log.Print(s.name)
		if s.layer != nil {
			synth.SetLayer("test", *s.layer, 1)
		} else if voices, ok := haptics.Effect(s.effect); ok {
			synth.Play(voices, 1)
		}
		if !wait(done, s.dur) {
			return
		}
		if s.layer != nil {
			synth.SetLayer("test", *s.layer, 0)
		}
		if !wait(done, 600*time.Millisecond) {
			return
		}
	}
	log.Print("Haptics test done.")
}

type hapticsStep struct {
	name   string
	layer  *haptics.Voice // a continuous voice, or
	effect string         // a one-shot
	dur    time.Duration
}

func hapticsSteps() []hapticsStep {
	on := func(v haptics.Voice, left, right float64) *haptics.Voice {
		v.L, v.R = left, right
		return &v
	}
	return []hapticsStep{
		{"1: LEFT actuator only, 60 Hz", on(haptics.Voice{Wave: haptics.Sine, F0: 60, Amp: 0.8}, 1, 0), "", 1500 * time.Millisecond},
		{"2: RIGHT actuator only, 150 Hz", on(haptics.Voice{Wave: haptics.Sine, F0: 150, Amp: 0.8}, 0, 1), "", 1500 * time.Millisecond},
		{"3: RIGHT multi-cannon texture", on(haptics.WeaponTexture("multicannon"), 0, 1), "", 2 * time.Second},
		{"4: LEFT beam laser texture", on(haptics.WeaponTexture("beam"), 1, 0), "", 2 * time.Second},
		{"5: boost", nil, "boost", 1500 * time.Millisecond},
		{"6: shields down", nil, "shields_down", 1500 * time.Millisecond},
		{"7: hull hit", nil, "hull_hit", time.Second},
		{"8: hardpoints deploying", nil, "hardpoints", time.Second},
		{"9: docking clamps", nil, "docked", 1500 * time.Millisecond},
	}
}
