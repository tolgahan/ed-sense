// Package backend is the seam between EDSense's brain (the game, the lights
// and the haptics) and what reaches the controller: DSX, or DS4Windows'
// DSX listener. The DSX client and the virtual DualSense fit these
// interfaces as they are.
package backend

import (
	"sync"

	"github.com/tolgahan/ed-sense/internal/ds4w"
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
// its controller profile; DS4Windows: following its profile). RequestReset
// is the tray's reset item. Gyro tells what the backend's own profile does
// with the gyro for Elite.
type Setup interface {
	Step()
	RequestReset()
	Gyro() GyroUse
}

// GameWatcher is Setup work that follows the game: each tick it hears
// whether Elite runs, whether EDSense's gyro aim is wanted, and whether
// Elite is in front.
type GameWatcher interface {
	Game(running, aim, front bool)
}

// Checker is Setup work whose first check runs in the background:
// Checked is closed when it is done.
type Checker interface {
	Checked() <-chan struct{}
}

// Reporter is Setup work that publishes what its last check found
// (DS4Windows: the profile in use and its checks); nil before the first.
type Reporter interface {
	Report() *ds4w.Report
}

// GyroUse is what the backend's own profile does with the gyro.
type GyroUse int

const (
	GyroUnknown   GyroUse = iota // it could not tell
	GyroMouse                    // motion to mouse
	GyroElsewhere                // a stick, keys or swipes
	GyroUnused                   // nothing
)

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
	// RumbleMutesHaptics: rumble puts the real controller in rumble
	// emulation, and its native haptics stay muted until a report switches
	// it off or the controller is plugged in again (DS4Windows keeps the
	// rumble bits of the last report in every report after it). Then only
	// haptics_mode "rumble" rumbles.
	RumbleMutesHaptics bool
}

// Backend is one way to reach the controller. Output, Pad, NewAudio and
// Close must be set; NewSetup is nil when there is nothing to set up.
// NewDSX and NewDS4Windows set Addr, and make Close safe to call again.
type Backend struct {
	Name     string // for the log and the tray
	Kind     Kind
	Words    Words  // what the player is told
	BiasFile string // the gyro calibration's file in the data folder
	Caps     Caps
	Output   Output
	Pad      Pad
	Motion   Motion // nil: no motion stream
	// NewAudio returns the native haptics output, fed by render. The app
	// calls it once, with its synth.
	NewAudio func(render func(frames []int16)) Audio
	// NewSetup returns the setup work; its files go under dataDir, and
	// notify tells the player.
	NewSetup func(dataDir string, notify func(string)) Setup
	// Close closes what the backend opened itself (DSX: the UDP socket),
	// after the app has closed Audio and Pad.
	Close func()
	// Addr is where the triggers and lights go now, such as
	// "127.0.0.1:6969"; "" when unknown.
	Addr func() string
}

// Discard closes a backend the app never attached: its pad, then what it
// opened itself. One the app attached is closed by the app.
func (b *Backend) Discard() {
	if b.Pad != nil {
		b.Pad.Close()
	}
	if b.Close != nil {
		b.Close()
	}
}

// closeOnce makes a Close safe to call again; nil stays nil.
func closeOnce(f func()) func() {
	if f == nil {
		return nil
	}
	return sync.OnceFunc(f)
}

// addrOf is Parts.Addr as the backend's Addr: "" when there is none.
func addrOf(addr func() string) func() string {
	return func() string {
		if addr == nil {
			return ""
		}
		return addr()
	}
}

// hidList is where the backends' links and checks look for the HID
// devices; tests give their own.
var hidList = dualsense.ListHID

// VirtualPad is the virtual DualSense the kind's backend would open, as
// the HID devices are now. It lists the devices, so it is for the setup
// checks, off the loop.
func VirtualPad(kind Kind) (dualsense.HIDDevice, bool) { return VirtualIn(hidList(), kind) }

// VirtualIn is the virtual DualSense the kind's backend would open among
// devs.
func VirtualIn(devs []dualsense.HIDDevice, kind Kind) (dualsense.HIDDevice, bool) {
	prefer := ""
	if kind == KindDS4Windows {
		prefer = ds4wLinkOptions(Words{}).Prefer
	}
	return dualsense.PickVirtual(devs, prefer)
}

// PhysicalPadVisible: games can see a real DualSense (HidHide does not
// hide it). It lists the devices, so it is off the loop.
func PhysicalPadVisible() bool { return PhysicalIn(hidList()) }

// PhysicalIn: devs has a real DualSense, which games can see.
func PhysicalIn(devs []dualsense.HIDDevice) bool {
	for _, d := range devs {
		if d.IsDualSense() && d.Kind == dualsense.Physical {
			return true
		}
	}
	return false
}

// HIDDevices lists the HID devices as the backends see them. It takes a
// moment: off the loop.
func HIDDevices() []dualsense.HIDDevice { return hidList() }

var (
	_ Output      = (*dsx.Client)(nil)
	_ Pad         = (*dualsense.Link)(nil)
	_ Audio       = (*dualsense.HapticsOut)(nil)
	_ Setup       = dsxSetup{}
	_ Setup       = ds4wSetup{}
	_ GameWatcher = ds4wSetup{}
	_ Checker     = ds4wSetup{}
	_ Reporter    = ds4wSetup{}
)
