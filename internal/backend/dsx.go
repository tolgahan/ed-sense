package backend

import (
	"path/filepath"

	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// DSXCaps: DSX does everything but keep its overrides.
func DSXCaps() Caps {
	return Caps{Triggers: true, Lightbar: true, PlayerLEDs: true, Mic: true, MotionOff: true, Rumble: true, Haptics: true, Gyro: true}
}

// DSX reaches the controller through DSX: the triggers and lights as UDP
// packets to its Mod System, the input, rumble and native haptics through
// its virtual DualSense.
func DSX(port int, verbose bool) (*Backend, error) {
	client, err := dsx.NewClient(port, verbose)
	if err != nil {
		return nil, err
	}
	link := dualsense.NewLink()
	return NewDSX(DSXParts{
		Output:  client,
		Pad:     link,
		Reports: link,
		Audio: func(render func(frames []int16)) Audio {
			return dualsense.NewHapticsOut(render)
		},
		Profile: func(backupDir string, notify func(string)) Setup {
			return dsx.NewProfileInstaller(backupDir, notify)
		},
		Close: client.Close,
	}), nil
}

// DSXParts are the devices behind the DSX backend; the golden tests give
// NewDSX recording ones.
type DSXParts struct {
	Output  Output
	Pad     Pad
	Reports Reports // the pad's reports as they arrive, for the gyro; nil: none
	Audio   func(render func(frames []int16)) Audio
	Profile func(backupDir string, notify func(string)) Setup
	Close   func()
}

// NewDSX puts the DSX backend together from its parts.
func NewDSX(p DSXParts) *Backend {
	b := &Backend{
		Name:     "DSX",
		Caps:     DSXCaps(),
		Output:   p.Output,
		Pad:      p.Pad,
		NewAudio: p.Audio,
		NewSetup: func(dataDir string, notify func(string)) Setup {
			return p.Profile(filepath.Join(dataDir, "dsx_profile_backups"), notify)
		},
		Close: p.Close,
	}
	if p.Reports != nil {
		b.Motion = dualSenseMotion{p.Reports}
	}
	return b
}
