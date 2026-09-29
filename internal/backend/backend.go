// Package backend is the seam between EDSense's brain (the game, the lights
// and the haptics) and what reaches the controller. DSX is the only backend
// so far, and its parts fit these interfaces as they are.
package backend

import (
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// Output takes the triggers and lights, switches the gyro's mouse output,
// and hands the controller back. Controllers are indices in the backend's
// own numbering.
type Output interface {
	Send(controllers []int, prev *dsx.Frame, next dsx.Frame, out dsx.Outputs)
	SetMotionOff(controllers []int, off bool)
	ResetToProfile(controllers []int)
	RequestStatus()
	Controllers() []int
	Online() bool
}

// Pad reads the controller and drives its rumble motors.
type Pad interface {
	Maintain()
	Available() bool
	State() dualsense.State
	SetRumble(left, right uint8)
	Close()
}

// Audio streams native haptics, pulling samples from the synth.
type Audio interface {
	Maintain()
	Active() bool
	Close()
}

// Setup is work a backend does outside the game, every few seconds (DSX:
// its controller profile).
type Setup interface {
	Step()
	RequestReset()
}

var (
	_ Output = (*dsx.Client)(nil)
	_ Pad    = (*dualsense.Link)(nil)
	_ Audio  = (*dualsense.HapticsOut)(nil)
	_ Setup  = (*dsx.ProfileInstaller)(nil)
)
