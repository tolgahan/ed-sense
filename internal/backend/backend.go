// Package backend is the seam between EDSense's brain (the game, the lights
// and the haptics) and what reaches the controller. DSX is the only backend
// so far, and its parts fit these interfaces as they are.
package backend

import (
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// The brain still speaks DSX's frame, motion modes and the DualSense's
// input report; these names spare it importing those packages.
type (
	Frame      = dsx.Frame
	Outputs    = dsx.Outputs
	Input      = dualsense.State
	MotionMode = dsx.MotionMode
)

const (
	MotionProfile  = dsx.MotionProfile
	MotionNone     = dsx.MotionNone
	MotionToMouse  = dsx.MotionToMouse
	MotionDisabled = dsx.MotionDisabled
)

// Output takes the triggers and lights, switches the backend's own gyro
// aim, and hands the controller back. Controllers are indices in the
// backend's own numbering.
type Output interface {
	Send(controllers []int, prev *Frame, next Frame, out Outputs)
	SetMotion(controllers []int, m MotionMode)
	ResetToProfile(controllers []int)
	RequestStatus()
	Controllers() []int
	Online() bool
}

// Pad reads the controller and drives its rumble motors.
type Pad interface {
	Maintain()
	Available() bool
	State() Input
	SetRumble(left, right uint8)
	Close()
}

// Audio streams native haptics, pulling samples from the synth on its own
// thread.
type Audio interface {
	Maintain()
	Active() bool
	Close()
}

// Motion streams the controller's motion, one sample per input report, on
// the backend's own goroutine. OnSample(nil) stops it.
type Motion interface {
	OnSample(f func(gyro.Sample))
}

// Setup is work a backend does outside the game, every few seconds (DSX:
// its controller profile). RequestReset is the tray's reset item.
// GyroToMouse tells whether the backend's own gyro aim for Elite is motion
// to mouse (known: it could tell), the only kind EDSense's gyro replaces.
type Setup interface {
	Step()
	RequestReset()
	GyroToMouse() (yes, known bool)
}

// Caps is what a backend can do; the brain leaves out the rest. What works
// right now is asked of the parts: Online, Available, Active.
type Caps struct {
	Triggers, Lightbar, PlayerLEDs, Mic bool // Output.Send shows them
	MotionOff                           bool // Output.SetMotion switches the backend's own gyro aim
	Rumble                              bool // Pad.SetRumble is felt
	Haptics                             bool // Audio plays native haptics
	Gyro                                bool // the controller has a gyro, in Pad.State and Motion
	// KeepsOverrides: what it was told stays until it is handed back, even
	// after EDSense dies. DSX forgets after a minute without packets.
	KeepsOverrides bool
}

// Backend is one way to reach the controller. Output, Pad, NewAudio and
// Close must be set; NewSetup is nil when there is nothing to set up.
type Backend struct {
	Name   string // for the log and the tray
	Caps   Caps
	Output Output
	Pad    Pad
	Motion Motion // nil: no motion stream
	// NewAudio returns the native haptics output, fed by render. The app
	// calls it once, with its synth.
	NewAudio func(render func(frames []int16)) Audio
	// NewSetup returns the setup work; its files go under dataDir, and
	// notify tells the player.
	NewSetup func(dataDir string, notify func(string)) Setup
	// Close closes what the backend opened itself (DSX: the UDP socket),
	// after the app has closed Audio and Pad.
	Close func()
}

var (
	_ Output = (*dsx.Client)(nil)
	_ Pad    = (*dualsense.Link)(nil)
	_ Audio  = (*dualsense.HapticsOut)(nil)
	_ Setup  = (*dsx.ProfileInstaller)(nil)
)
