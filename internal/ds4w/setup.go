package ds4w

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Warning is something in DS4Windows' setup that keeps EDSense from
// working fully. Each is told once.
type Warning int

const (
	WarnNotDualSense    Warning = iota // the profile emulates another controller
	WarnPhysicalVisible                // games see the real controller too (HidHide off)
	WarnDSXOnPort                      // DSX answers on DS4Windows' port
	WarnOldVersion                     // DS4Windows before 5 has no DSX listener
	WarnTriggerLab                     // Trigger Lab sets a trigger, over EDSense's
	WarnTouchpadMouse                  // the profile moves the cursor with the touchpad
)

// Env is how Setup reaches DS4Windows and the system. Tests give their
// own; nil ones know nothing.
type Env struct {
	DataDir         func() string                               // DS4Windows' data folder, "" when unknown
	Query           func(slot int, prop string) (string, error) // asks the running DS4Windows
	Elite           func() string                               // the game's exe: its full path, or its name when the path is unknown
	Slot            func() int                                  // the DS4Windows slot EDSense drives
	MAC             func() string                               // that controller's MAC address, as DS4Windows reports it; "" when unknown
	Version         func() string                               // DS4Windows' version, "" when unknown
	DSXOnPort       func() bool                                 // DSX answers on DS4Windows' port
	PhysicalVisible func() bool                                 // a real DualSense is visible to games
}

// Setup follows the DS4Windows profile of the controller EDSense drives:
// what it does with the gyro, and settings that keep EDSense from working
// fully. The checks run on a goroutine of their own, since asking
// DS4Windows can take a second; Step and Game only ask for one.
type Setup struct {
	env     Env
	warn    func(w Warning, detail string)
	kick    chan bool     // true: the first check, with the one-time ones
	checked chan struct{} // closed when the first check is done

	mu                  sync.Mutex
	gyro                Gyro
	running, aim, front bool

	// the checks' own
	warned map[Warning]bool
	cache  map[string]cached
	said   string // the profile line logged last
	whyNot string // why DS4Windows could not be asked, logged once
}

type cached struct {
	mod  time.Time
	size int64
	p    Profile
	err  error
}

// NewSetup starts following the profile, with a first check at once and
// the one-time ones. warn tells the player.
func NewSetup(env Env, warn func(w Warning, detail string)) *Setup {
	s := newSetup(env, warn)
	s.request(true)
	go func() {
		for first := range s.kick {
			s.check(first)
			if first {
				close(s.checked)
			}
		}
	}()
	return s
}

// Checked is closed when the first check is done, and Gyro tells what it
// found.
func (s *Setup) Checked() <-chan struct{} { return s.checked }

func newSetup(env Env, warn func(w Warning, detail string)) *Setup {
	none := func() string { return "" }
	no := func() bool { return false }
	if env.DataDir == nil {
		env.DataDir = none
	}
	if env.Elite == nil {
		env.Elite = func() string { return "EliteDangerous64.exe" }
	}
	if env.Slot == nil {
		env.Slot = func() int { return 0 }
	}
	if env.MAC == nil {
		env.MAC = none
	}
	if env.Version == nil {
		env.Version = none
	}
	if env.DSXOnPort == nil {
		env.DSXOnPort = no
	}
	if env.PhysicalVisible == nil {
		env.PhysicalVisible = no
	}
	return &Setup{env: env, warn: warn, kick: make(chan bool, 1), checked: make(chan struct{}), warned: map[Warning]bool{}, cache: map[string]cached{}}
}

// Step runs every few seconds on the loop: it checks the profile again
// while Elite runs with gyro aim on.
func (s *Setup) Step() {
	s.mu.Lock()
	again := s.running && s.aim
	s.mu.Unlock()
	if again {
		s.request(false)
	}
}

// Game tells, each tick, whether Elite runs, whether EDSense's gyro aim is
// wanted, and whether Elite is in front. Elite coming to the front is
// checked at once, since Auto Profiles switch profiles then.
func (s *Setup) Game(running, aim, front bool) {
	s.mu.Lock()
	rose := front && !s.front
	s.running, s.aim, s.front = running, aim, front
	s.mu.Unlock()
	if rose && running && aim {
		s.request(false)
	}
}

func (s *Setup) request(first bool) {
	select {
	case s.kick <- first:
	default: // one is waiting
	}
}

// RequestReset: DS4Windows' profiles are left alone.
func (s *Setup) RequestReset() {}

// Gyro is what the profile did with the gyro at the last check.
func (s *Setup) Gyro() Gyro {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gyro
}

// check reads the profile the controller uses and what it does with the
// gyro, and logs it when that changes.
func (s *Setup) check(first bool) {
	dir := s.env.DataDir()
	if first {
		s.oneTime(dir)
	}
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	slot := s.env.Slot()
	if slot < 0 || slot >= slots {
		slot = 0
	}
	name, source, asked := s.profileName(dir, slot, running)
	g, what := GyroUnknown, source
	if name != "" {
		p, err := s.readProfile(dir, name)
		if err != nil {
			what = fmt.Sprintf("%s; %v", source, err)
		} else {
			g, what = p.Gyro(), fmt.Sprintf("%s; gyro output %s", source, p.GyroOutput)
			output := p.Output
			if asked {
				if o, err := s.env.Query(slot, PropOutput); err == nil && o != "" {
					output = o
				}
			}
			if !EmulatesDualSense(output) {
				s.once(WarnNotDualSense, fmt.Sprintf("%s (%s)", name, output))
			}
			if p.TriggerLab {
				s.once(WarnTriggerLab, name)
			}
			if p.TouchpadMouse() {
				s.once(WarnTouchpadMouse, name)
			}
		}
	}
	line := fmt.Sprintf("DS4Windows: controller %d, profile %q (%s): the gyro is %s", slot+1, name, what, g)
	if line != s.said {
		log.Print(line)
		s.said = line
	}
	s.mu.Lock()
	s.gyro = g
	s.mu.Unlock()
}

// profileName: the profile of the controller in slot, and where it was
// found. DS4Windows itself knows best (linked and temporary profiles
// too); else, as DS4Windows picks it, "Auto Profiles.xml" while Elite
// runs, then the profile linked to the controller, then Profiles.xml.
func (s *Setup) profileName(dir string, slot int, running bool) (name, source string, asked bool) {
	if s.env.Query != nil {
		name, err := s.env.Query(slot, PropProfile)
		if err == nil && name == "" {
			err = errors.New("it named no profile")
		}
		if err == nil {
			s.whyNot = ""
			return name, "DS4Windows says so", true
		}
		if err.Error() != s.whyNot {
			log.Printf("DS4Windows: could not ask it for the profile (%v), so its files are read", err)
			s.whyNot = err.Error()
		}
	}
	if dir == "" {
		return "", "DS4Windows' folder not found", false
	}
	if running {
		if rules, err := ReadAutoProfiles(dir); err == nil {
			name, ok := AutoProfileFor(rules, s.env.Elite(), slot)
			if !ok {
				return "", "a window title in " + autoProfilesFile + " may give Elite another profile", false
			}
			if name != "" {
				return name, "from " + autoProfilesFile, false
			}
		}
	}
	// DS4Windows writes the profile from before the link to Profiles.xml
	if links, err := ReadLinkedProfiles(dir); err == nil && len(links) > 0 {
		mac := s.env.MAC()
		if mac == "" {
			return "", linkedProfilesFile + " links profiles, and the controller's MAC address is not known yet", false
		}
		if name := LinkedProfile(links, mac); name != "" {
			return name, "linked in " + linkedProfilesFile, false
		}
	}
	st, err := ReadSettings(dir)
	if err != nil {
		return "", err.Error(), false
	}
	if st.Controllers[slot] == "" {
		return "", settingsFile + " names no profile", false
	}
	return st.Controllers[slot], "from " + settingsFile, false
}

// readProfile reads a profile file, again only when it changed.
func (s *Setup) readProfile(dir, name string) (Profile, error) {
	path, err := FindProfile(dir, name)
	if err != nil {
		return Profile{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return Profile{}, err
	}
	if c, ok := s.cache[path]; ok && c.mod.Equal(st.ModTime()) && c.size == st.Size() {
		return c.p, c.err
	}
	p, err := ReadProfile(path)
	s.cache[path] = cached{st.ModTime(), st.Size(), p, err}
	return p, err
}

// oneTime: the checks made once, when EDSense starts.
func (s *Setup) oneTime(dir string) {
	v := s.env.Version()
	if v == "" && dir != "" {
		if st, err := ReadSettings(dir); err == nil {
			v = st.AppVersion
		}
	}
	if major, ok := Major(v); ok && major < 5 {
		s.once(WarnOldVersion, v)
	}
	if s.env.DSXOnPort() {
		s.once(WarnDSXOnPort, "")
	}
	if s.env.PhysicalVisible() {
		s.once(WarnPhysicalVisible, "")
	}
}

func (s *Setup) once(w Warning, detail string) {
	if s.warned[w] {
		return
	}
	s.warned[w] = true
	if s.warn != nil {
		s.warn(w, detail)
	}
}
