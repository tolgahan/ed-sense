package backend

import (
	"path/filepath"

	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/gyro"
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
	return NewDSX(Parts{
		Output:  client,
		Pad:     link,
		Reports: link,
		Audio: func(render func(frames []int16)) Audio {
			return dualsense.NewHapticsOut(render)
		},
		Profile: func(backupDir string, notify func(string)) Setup {
			return dsxSetup{dsx.NewProfileInstaller(backupDir, notify)}
		},
		Close: client.Close,
	}), nil
}

// Parts are the devices behind a backend; the golden tests give NewDSX
// and NewDS4Windows recording ones.
type Parts struct {
	Output  Output
	Pad     Pad
	Reports Reports // the pad's reports as they arrive, for the gyro; nil: none
	Audio   func(render func(frames []int16)) Audio
	Profile func(dir string, notify func(string)) Setup // DSX: dir is for backups
	Close   func()
}

// DSXParts is the name Parts had while DSX was the only backend.
type DSXParts = Parts

// NewDSX puts the DSX backend together from its parts.
func NewDSX(p Parts) *Backend {
	b := &Backend{
		Name:     "DSX",
		Kind:     KindDSX,
		Words:    DSXWords(),
		BiasFile: gyro.BiasFile,
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
		b.Motion = dualSenseMotion{r: p.Reports, lsb: dsGyroLSB}
	}
	return b
}

// dsxSetup is DSX's profile installer as the backend's setup.
type dsxSetup struct{ *dsx.ProfileInstaller }

func (s dsxSetup) Gyro() GyroUse { return GyroUseOf(s.GyroToMouse()) }

// GyroUseOf: DSX tells only whether its profile's gyro is motion to mouse.
func GyroUseOf(mouse, known bool) GyroUse {
	switch {
	case !known:
		return GyroUnknown
	case mouse:
		return GyroMouse
	}
	return GyroElsewhere
}
