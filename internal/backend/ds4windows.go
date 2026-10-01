package backend

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/platform"
)

// DS4WindowsCaps: DS4Windows does everything but switch its own gyro aim
// off, and keeps what it was told until it is handed back, even after
// EDSense dies.
func DS4WindowsCaps() Caps {
	c := DSXCaps()
	c.MotionOff, c.KeepsOverrides, c.RumbleMutesHaptics = false, true, true
	return c
}

// DS4WindowsBiasFile keeps the gyro calibration for DS4Windows' virtual
// DualSense, whose counts differ from DSX's.
const DS4WindowsBiasFile = "gyro_calibration_ds4windows.json"

// DS4WindowsOptions set up the DS4Windows backend.
type DS4WindowsOptions struct {
	Addr    *net.UDPAddr  // DS4Windows' DSX listener
	Port    int           // ds4windows_port: 0 reads it from DS4Windows' settings
	Follow  bool          // look the listener up again while DS4Windows does not answer
	Verbose bool          // log every packet
	Haptics func() string // ds4windows_haptics as it is now
	Warned  *ds4w.Once    // the setup warnings told, shared by every DS4Windows backend of the process; nil: this backend's own
}

// DS4Windows reaches the controller through DS4Windows 5: the triggers and
// lights as UDP packets to its DSX listener, the input, rumble and native
// haptics through its virtual DualSense (VIIPER, under usbip-win2), or the
// haptics through the controller's own audio device.
func DS4Windows(o DS4WindowsOptions) (*Backend, error) {
	client, err := dsx.NewClientTo(o.Addr, o.Verbose, dsx.DS4Windows)
	if err != nil {
		return nil, err
	}
	w := DS4WindowsWords()
	link := dualsense.NewLinkFor(ds4wLinkOptions(w))
	route := func() dualsense.AudioRoute {
		if o.Haptics == nil {
			return dualsense.RouteAuto
		}
		return HapticsRoute(o.Haptics())
	}
	env := ds4wEnv(client, o)
	closeAll := client.Close
	if o.Follow {
		stop := make(chan struct{})
		listener := func() (*net.UDPAddr, string) { return DS4WindowsListener(o.Port) }
		go followListener(client, DS4WindowsExePath, listener, 3*time.Second, stop)
		closeAll = sync.OnceFunc(func() {
			close(stop)
			client.Close()
		})
	}
	return NewDS4Windows(Parts{
		Output:  client,
		Pad:     link,
		Reports: link,
		Audio: func(render func(frames []int16)) Audio {
			return dualsense.NewHapticsOutFor(render, dualsense.AudioOptions{Route: route, Prefer: dualsense.HostUSBIPWin2, Missing: w.AudioMissing})
		},
		Profile: func(_ string, notify func(string)) Setup {
			return ds4wSetup{ds4w.NewSetup(env, func(which ds4w.Warning, detail string) {
				text := w.Warning(which, detail)
				log.Printf("DS4Windows: %s", strings.Join(strings.Fields(text), " "))
				notify(text)
			})}
		},
		Close: closeAll,
		Addr:  client.Addr,
	}), nil
}

// where DS4Windows' setup reads DS4Windows' settings and asks it; tests
// stub them
var (
	ds4wDataDir = DS4WindowsDataDir
	ds4wQuery   = func(slot int, prop string) (string, error) {
		return ds4w.Query(ds4w.DS4WindowsNames, slot, prop)
	}
)

// ds4wEnv is how DS4Windows' setup reaches DS4Windows and the system. Its
// warnings are told once per o.Warned, else once per backend.
func ds4wEnv(client *dsx.Client, o DS4WindowsOptions) ds4w.Env {
	slot := func() int {
		if c := client.Controllers(); len(c) > 0 {
			return c[0]
		}
		return 0
	}
	env := ds4w.Env{
		DataDir: ds4wDataDir,
		Query:   ds4wQuery,
		Elite: func() string {
			if p := platform.ProcessPath(elite.GameExe); p != "" {
				return p
			}
			return elite.GameExe
		},
		Slot:    slot,
		MAC:     func() string { return client.MAC(slot()) },
		Version: DS4WindowsVersion,
		DSXOnPort: func() bool {
			d, ok := dsx.Probe(o.Addr, ProbeTimeout)
			return ok && d == dsx.DSX
		},
		PhysicalVisible: PhysicalPadVisible,
		Warned:          o.Warned,
	}
	if env.Warned == nil {
		env.Warned = &ds4w.Once{}
	}
	return env
}

// ds4wLinkOptions: DS4Windows' virtual DualSense, under usbip-win2. Its
// stops switch rumble emulation off, since DS4Windows keeps the rumble bits
// of the last report.
func ds4wLinkOptions(w Words) dualsense.LinkOptions {
	return dualsense.LinkOptions{Prefer: dualsense.HostUSBIPWin2, Missing: w.PadMissing, StopClears: true, List: hidList}
}

// followListener: while DS4Windows does not answer, its listener is looked
// up again whenever the DS4Windows that runs changes (checked every so
// often), since one that starts after EDSense may keep its settings
// elsewhere (a portable one, in its own folder). exe is the running
// DS4Windows' exe, "" when none runs; listener reads where it listens, and
// the data folder.
func followListener(client *dsx.Client, exe func() string, listener func() (*net.UDPAddr, string), every time.Duration, stop <-chan struct{}) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	last := exe() // the DS4Windows the address was read for
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		if client.Online() {
			continue
		}
		now := exe()
		if now == last {
			continue
		}
		last = now
		addr, dir := listener()
		if changed, err := client.Retarget(addr); err != nil {
			log.Printf("DS4Windows: cannot reach %s: %v", addr, err)
		} else if changed {
			log.Printf("DS4Windows: its listener is on %s now (%s)", addr, DS4WindowsSettingsFrom(dir))
		}
	}
}

// NewDS4Windows puts the DS4Windows backend together from its parts.
func NewDS4Windows(p Parts) *Backend {
	b := &Backend{
		Name:     "DS4Windows",
		Kind:     KindDS4Windows,
		Words:    DS4WindowsWords(),
		BiasFile: DS4WindowsBiasFile,
		Caps:     DS4WindowsCaps(),
		Output:   p.Output,
		Pad:      p.Pad,
		NewAudio: p.Audio,
		NewSetup: p.Profile,
		Close:    closeOnce(p.Close),
		Addr:     addrOf(p.Addr),
	}
	if p.Reports != nil {
		b.Motion = dualSenseMotion{r: p.Reports, lsb: viiperGyroLSB, viiper: true}
	}
	return b
}

// HapticsRoute is ds4windows_haptics as an audio route.
func HapticsRoute(setting string) dualsense.AudioRoute {
	switch setting {
	case config.DS4WHapticsController:
		return dualsense.RouteController
	case config.DS4WHapticsVirtual:
		return dualsense.RouteVirtual
	}
	return dualsense.RouteAuto
}

// DS4WindowsExePath is the exe of the DS4Windows that runs, or "": by its
// name, else by its window, which a renamed exe keeps.
func DS4WindowsExePath() string {
	if p := platform.ProcessPath(ds4w.Exe); p != "" {
		return p
	}
	p, _ := ds4w.WindowProcess(ds4w.DS4WindowsNames)
	return p
}

// DS4WindowsRunning: DS4Windows' window is there, whatever its exe is
// called.
func DS4WindowsRunning() bool {
	_, ok := ds4w.WindowProcess(ds4w.DS4WindowsNames)
	return ok
}

// DS4WindowsVersion is the version of the DS4Windows that runs, or "".
func DS4WindowsVersion() string {
	if p := DS4WindowsExePath(); p != "" {
		return ds4w.ExeVersion(p)
	}
	return ""
}

// DS4WindowsDataDir is where the DS4Windows that runs, or the installed
// one, keeps its settings.
func DS4WindowsDataDir() string {
	exeDir := ""
	if p := DS4WindowsExePath(); p != "" {
		exeDir = filepath.Dir(p)
	}
	return ds4w.DataDir(exeDir, os.Getenv("APPDATA"))
}

// DS4WindowsListener is where DS4Windows' DSX listener listens: its own
// setting, with port in place of its port when that is not 0. dir is the
// data folder whose Profiles.xml gave it: "" when none was read, and the
// default is used.
func DS4WindowsListener(port int) (addr *net.UDPAddr, dir string) {
	dir = DS4WindowsDataDir()
	s, err := ds4w.ReadSettings(dir)
	if err != nil {
		dir = ""
	}
	return s.Endpoint(port), dir
}

// DS4WindowsSettingsFrom says, for the log, where DS4WindowsListener read
// the listener's address.
func DS4WindowsSettingsFrom(dir string) string {
	if dir == "" {
		return "DS4Windows' settings not found, so its default"
	}
	return "from " + filepath.Join(dir, "Profiles.xml")
}

// ds4wSetup follows DS4Windows' profile as the backend's setup.
type ds4wSetup struct{ *ds4w.Setup }

func (s ds4wSetup) Gyro() GyroUse {
	switch s.Setup.Gyro() {
	case ds4w.GyroFree:
		return GyroUnused
	case ds4w.GyroMouse:
		return GyroMouse
	case ds4w.GyroOther:
		return GyroElsewhere
	}
	return GyroUnknown
}
