package backend

import (
	"path/filepath"
	"sync"

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
	return DSXWith(DSXOptions{Port: port, Verbose: verbose})
}

// DSXOptions set up the DSX backend.
type DSXOptions struct {
	Port     int
	Verbose  bool        // log every packet
	FirstAdd func() bool // EDSense's DSX profile may be added when DSX has none; nil: always
	Profile  *DSXProfile // the installer every DSX backend of the process shares; nil: one per setup
}

// DSXProfile is the one DSX profile installer of a process, shared by its
// DSX backends and sessions: the first check runs once, and a reset that
// waits for DSX to be closed outlives a switch or Apply now. Only the loop
// uses the installer. The zero value is ready.
type DSXProfile struct {
	mu sync.Mutex
	p  *dsx.ProfileInstaller
}

// installer is the shared installer, made by the first setup that asks
// for it.
func (s *DSXProfile) installer(backupDir string, notify func(string), firstAdd func() bool) *dsx.ProfileInstaller {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p == nil {
		s.p = dsx.NewProfileInstaller(backupDir, notify)
		s.p.FirstAdd = firstAdd
	}
	return s.p
}

// DSXWith is DSX with all its options.
func DSXWith(o DSXOptions) (*Backend, error) {
	client, err := dsx.NewClient(o.Port, o.Verbose)
	if err != nil {
		return nil, err
	}
	link := dualsense.NewLinkFor(dualsense.LinkOptions{List: hidList})
	return NewDSX(Parts{
		Output:  client,
		Pad:     link,
		Reports: link,
		Audio: func(render func(frames []int16)) Audio {
			return dualsense.NewHapticsOut(render)
		},
		Profile: func(backupDir string, notify func(string)) Setup {
			if o.Profile != nil {
				return dsxSetup{o.Profile.installer(backupDir, notify, o.FirstAdd)}
			}
			p := dsx.NewProfileInstaller(backupDir, notify)
			p.FirstAdd = o.FirstAdd
			return dsxSetup{p}
		},
		Close: client.Close,
		Addr:  client.Addr,
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
	Addr    func() string // where the triggers and lights go now; nil: unknown
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
		Close: closeOnce(p.Close),
		Addr:  addrOf(p.Addr),
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
