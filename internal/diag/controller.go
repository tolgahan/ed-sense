// Package diag holds the command-line checks: testing the virtual DualSense's
// rumble and input.
package diag

import (
	"fmt"
	"log"
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
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
// side, and shows the buttons and triggers.
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
	log.Print("Input: press R2, L2, R1 and Circle (15 s)")
	_ = pad.State()
	last := ""
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); {
		st := pad.State()
		line := "no input reports yet"
		if st.OK {
			line = fmt.Sprintf("L2 %3d  R2 %3d  R1 %v  Circle %v",
				st.L2/16*16, st.R2/16*16, st.Held(dualsense.R1), st.Held(dualsense.Circle))
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
