package ds4w

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
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

// Once remembers the warnings told, so each is told once however many
// Setups come and go. The zero value is ready, and it is safe for
// concurrent use.
type Once struct {
	mu   sync.Mutex
	told map[Warning]bool
}

// First reports whether w was not told yet, and counts it as told.
func (o *Once) First(w Warning) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.told[w] {
		return false
	}
	if o.told == nil {
		o.told = map[Warning]bool{}
	}
	o.told[w] = true
	return true
}

// Report is what the last check found, for the window. Nobody changes a
// Report once it is published.
type Report struct {
	Slot       int    // the controller's DS4Windows slot, from 0
	Profile    string // the profile's name; "" when it was not found
	Source     string // where the name came from, or why it was not found
	Problem    string // why the profile file could not be read; "" when it was
	Output     string // the emulated controller: DS4Windows' answer, else the profile's
	Gyro       Gyro
	Touchpad   string // the touchpad's output mode
	TriggerLab bool
	Warnings   []Warning // what keeps EDSense from working fully, as the checks found it

	// Front: the check was made while Elite had been in front a while, so
	// the profile is the one Elite gets. Elite is the last check that was,
	// kept by one that was not; nil when there is none.
	Front bool
	Elite *Report
}

// ByRule: the profile was read from an Auto Profiles rule, so it is the
// one a rule gives, not the controller's usual one.
func (r *Report) ByRule() bool { return r.Source == "from "+autoProfilesFile }

// ForElite is the last check made while Elite was in front: r itself, or
// the one r keeps; nil when there is none.
func (r *Report) ForElite() *Report {
	if r == nil || r.Front {
		return r
	}
	return r.Elite
}

// frontSettles: DS4Windows' Auto Profiles look at the window in front
// every second, so a check this long after Elite came to the front reads
// the profile they give Elite.
const frontSettles = 2 * time.Second

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
	Warned          *Once                                       // the warnings told, shared by the Setups of one process; nil: this Setup's own
	Now             func() time.Time                            // nil: time.Now
}

// Setup follows the DS4Windows profile of the controller EDSense drives:
// what it does with the gyro, and settings that keep EDSense from working
// fully. The checks run on a goroutine of their own, since asking
// DS4Windows can take a second; Step and Game only ask for one. Close ends
// it.
type Setup struct {
	env     Env
	warn    func(w Warning, detail string)
	kick    chan bool     // true: the first check, with the one-time ones
	checked chan struct{} // closed when the first check is done
	stop    chan struct{} // closed by Close
	report  atomic.Pointer[Report]

	mu                  sync.Mutex
	gyro                Gyro
	running, aim, front bool
	frontSince          time.Time // when Elite last came to the front

	wmu    sync.Mutex  // held while a warning is told, and by Close
	closed atomic.Bool // after Close no check is asked for, and nothing is told, logged or published

	// the checks' own
	told    *Once
	lasting []Warning // found by the one-time checks
	cache   map[string]cached
	said    string  // the profile line logged last
	whyNot  string  // why DS4Windows could not be asked, logged once
	elite   *Report // the last check made while Elite was in front
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
	go s.work()
	return s
}

// work runs the checks asked for, until Close.
func (s *Setup) work() {
	for {
		select {
		case <-s.stop:
			return
		case first := <-s.kick:
			if s.closed.Load() {
				return
			}
			s.check(first)
			if first {
				close(s.checked)
			}
		}
	}
}

// Close ends the checks. It does not wait for one under way, which then
// tells, logs and publishes nothing. Checked stays open when the first
// check had not run. Calling it again does nothing.
func (s *Setup) Close() {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if !s.closed.Swap(true) {
		close(s.stop)
	}
}

// Checked is closed when the first check is done, and Gyro tells what it
// found.
func (s *Setup) Checked() <-chan struct{} { return s.checked }

// Report is what the last check found; nil before the first one.
func (s *Setup) Report() *Report { return s.report.Load() }

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
	if env.Now == nil {
		env.Now = time.Now
	}
	told := env.Warned
	if told == nil {
		told = &Once{}
	}
	return &Setup{env: env, warn: warn, kick: make(chan bool, 1), checked: make(chan struct{}), stop: make(chan struct{}), told: told, cache: map[string]cached{}}
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
	if rose {
		s.frontSince = s.env.Now()
	}
	s.running, s.aim, s.front = running, aim, front
	s.mu.Unlock()
	if rose && running && aim {
		s.request(false)
	}
}

// inFront: Elite runs in front, and came there at least frontSettles
// before now; since is when it came. Under mu.
func (s *Setup) inFront(now time.Time) (ok bool, since time.Time) {
	return s.running && s.front && now.Sub(s.frontSince) >= frontSettles, s.frontSince
}

// CheckNow asks for a check at once, whatever Elite does: the window's
// "Check again". It never waits, and does nothing after Close.
func (s *Setup) CheckNow() { s.request(false) }

func (s *Setup) request(first bool) {
	if s.closed.Load() {
		return
	}
	select {
	case s.kick <- first:
	default: // one is waiting
	}
}

// Gyro is what the profile did with the gyro at the last check.
func (s *Setup) Gyro() Gyro {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gyro
}

// check reads the profile the controller uses and what it does with the
// gyro, logs it when that changes, and publishes the Report.
func (s *Setup) check(first bool) {
	dir := s.env.DataDir()
	if first {
		s.oneTime(dir)
	}
	r := &Report{}
	found := append([]Warning(nil), s.lasting...)
	s.mu.Lock()
	running := s.running
	front, since := s.inFront(s.env.Now())
	s.mu.Unlock()
	slot := s.env.Slot()
	if slot < 0 || slot >= slots {
		slot = 0
	}
	name, source, asked := s.profileName(dir, slot, running)
	r.Slot, r.Profile, r.Source = slot, name, source
	g, what := GyroUnknown, source
	if name != "" {
		p, err := s.readProfile(dir, name)
		if err != nil {
			what = fmt.Sprintf("%s; %v", source, err)
			r.Problem = err.Error()
		} else {
			g, what = p.Gyro(), fmt.Sprintf("%s; gyro output %s", source, p.GyroOutput)
			output := p.Output
			if asked {
				if o, err := s.env.Query(slot, PropOutput); err == nil && o != "" {
					output = o
				}
			}
			r.Output, r.Touchpad, r.TriggerLab = output, p.Touchpad, p.TriggerLab
			if !EmulatesDualSense(output) {
				found = append(found, WarnNotDualSense)
				s.once(WarnNotDualSense, fmt.Sprintf("%s (%s)", name, output))
			}
			if p.TriggerLab {
				found = append(found, WarnTriggerLab)
				s.once(WarnTriggerLab, name)
			}
			if p.TouchpadMouse() {
				found = append(found, WarnTouchpadMouse)
				s.once(WarnTouchpadMouse, name)
			}
		}
	}
	if s.closed.Load() {
		return
	}
	line := fmt.Sprintf("DS4Windows: controller %d, profile %q (%s): the gyro is %s", slot+1, name, what, g)
	if line != s.said {
		log.Print(line)
		s.said = line
	}
	s.mu.Lock()
	s.gyro = g
	// still in front, since the same time
	front = front && s.running && s.front && s.frontSince.Equal(since)
	s.mu.Unlock()
	r.Gyro, r.Warnings, r.Front = g, found, front
	if front {
		s.elite = r
	} else {
		r.Elite = s.elite
	}
	s.report.Store(r)
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

// oneTime: the checks made once, when the Setup starts.
func (s *Setup) oneTime(dir string) {
	s.lasting = nil
	v := s.env.Version()
	if v == "" && dir != "" {
		if st, err := ReadSettings(dir); err == nil {
			v = st.AppVersion
		}
	}
	if major, ok := Major(v); ok && major < 5 {
		s.lasting = append(s.lasting, WarnOldVersion)
		s.once(WarnOldVersion, v)
	}
	if s.env.DSXOnPort() {
		s.lasting = append(s.lasting, WarnDSXOnPort)
		s.once(WarnDSXOnPort, "")
	}
	if s.env.PhysicalVisible() {
		s.lasting = append(s.lasting, WarnPhysicalVisible)
		s.once(WarnPhysicalVisible, "")
	}
}

// once tells w unless it was told before, or the Setup is closed.
func (s *Setup) once(w Warning, detail string) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.closed.Load() || !s.told.First(w) {
		return
	}
	if s.warn != nil {
		s.warn(w, detail)
	}
}
