package install

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/wakeup"
)

// Options set up a Service. The functions after Port say how the system
// is seen; nil ones look at the system itself.
type Options struct {
	DataDir  string           // EDSense's data folder: the copies go in it
	Notify   func(msg string) // tells the player; nil: nobody
	Kind     func() string    // the app EDSense uses now: control.AppDSX or control.AppDS4Windows
	FirstAdd func() bool      // DSX's first add may run: the first run is over, and DSX is chosen or surely runs; nil: never
	// Watch wakes when Kind or FirstAdd may have changed (the engine's
	// Watch); nil: never.
	Watch  func() (wake <-chan struct{}, stop func())
	Report func() *ds4w.Report // what the DS4Windows session found; nil: nothing
	Port   func() int          // ds4windows_port in the settings; nil: 0

	DSX    *dsx.Installer
	DS4    *ds4w.Installer
	DS4Exe func() (exe, version string) // the DS4Windows that runs; "" when none
	Elite  func() []string              // Elite's exes
	Env    func(key string) string      // APPDATA and USERPROFILE
	Now    func() time.Time
	Every  time.Duration // the tick while a job waits; 0: 1 s
	Delay  time.Duration // DSX's first add after DSX became the app, and again while its folder is not found; 0: 3 s
	Sample time.Duration // how often it looks for a DS4Windows that runs, for its data folder; 0: 5 s
}

// lookKeep is how long a look at an app's files is kept.
const lookKeep = time.Second

// ErrBusy: the app's profile is being written, so nothing was asked.
var ErrBusy = errors.New("EDSense is writing the profile")

// ErrChanged: the question the player answered is not the one the card
// asks now (the files or the folder changed), so nothing was asked.
var ErrChanged = errors.New("what it would write changed since the question was shown; look at the card, and ask again")

// busyError is an installer's busy error, which is ErrBusy too.
type busyError struct{ error }

func (busyError) Is(target error) bool { return target == ErrBusy }
func (e busyError) Unwrap() error      { return e.error }

// Service owns the profile jobs, one per app: EDSense's DSX profile and
// its DS4Windows profile for Elite. Its goroutine ticks every second while
// a job waits for the app to be closed, and sleeps otherwise, but for a
// look every few seconds at which DS4Windows runs, whose data folder an
// install uses. It adds DSX's profile by itself once (the first add). The
// cards it makes are published (Profiles, Watch) for the window's
// "profile" event. Nothing runs on the loop.
type Service struct {
	o    Options
	dsx  *dsx.Installer
	ds4  *ds4w.Installer
	w    wakeup.Group
	kick chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once

	dsxLook, ds4Look sync.Mutex // one look at an app's files at a time

	mu        sync.Mutex
	started   bool // the goroutine runs
	closed    bool
	rev       int64
	states    map[string]control.ProfileState // published, by app
	dsxSeen   dsxSeen
	ds4Seen   ds4Seen
	ds4Dir    string // DS4Windows' data folder, as seen while it ran
	ds4Also   string // another folder with its settings: which one it uses is not known
	ds4ExeDir string // and its exe's folder
	ds4Asked  int    // DS4Windows installs asked for
	ds4Ended  int    // the last of them whose end was told, and seen through: DS4Windows ran since
	onDSX     bool   // DSX was the app at the last tick
	dsxSince  time.Time
	firstDone bool      // DSX's first add has looked at DSX's profiles
	firstNext time.Time // when it tries again to find DSX's folder
	seeNext   time.Time // when it looks for a DS4Windows that runs again
}

// New makes the Service and starts it.
func New(o Options) *Service {
	s := Make(o)
	s.Start()
	return s
}

// Make makes the Service without starting its goroutine: Start does, once
// this is the EDSense that runs. Its methods work before that.
func Make(o Options) *Service {
	if o.Kind == nil {
		o.Kind = func() string { return "" }
	}
	if o.FirstAdd == nil {
		o.FirstAdd = func() bool { return false }
	}
	if o.Port == nil {
		o.Port = func() int { return 0 }
	}
	if o.Env == nil {
		o.Env = os.Getenv
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Every <= 0 {
		o.Every = time.Second
	}
	if o.Delay <= 0 {
		o.Delay = 3 * time.Second
	}
	if o.Sample <= 0 {
		o.Sample = 5 * time.Second
	}
	if o.DS4Exe == nil {
		o.DS4Exe = runningDS4Windows
	}
	if o.Elite == nil {
		o.Elite = dsx.EliteExes
	}
	if o.DSX == nil {
		o.DSX = dsx.NewInstaller(dsx.InstallerOptions{Backups: filepath.Join(o.DataDir, DSXBackups), Notify: o.Notify})
	}
	if o.DS4 == nil {
		o.DS4 = ds4w.NewInstaller(ds4w.InstallerOptions{Backups: filepath.Join(o.DataDir, DS4WindowsBackups)})
	}
	return &Service{o: o, dsx: o.DSX, ds4: o.DS4, kick: make(chan struct{}, 1), stop: make(chan struct{}),
		done: make(chan struct{}), states: map[string]control.ProfileState{}}
}

// Start starts the goroutine: the first add, the ticks of the jobs, the
// looks at DS4Windows. A second Start, or one after Close, does nothing.
func (s *Service) Start() {
	s.mu.Lock()
	if s.started || s.closed {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	wake, unwatch := (<-chan struct{})(nil), func() {}
	if s.o.Watch != nil {
		wake, unwatch = s.o.Watch()
	}
	go s.run(wake, unwatch)
}

// runningDS4Windows is the exe of the DS4Windows that runs, and its
// version.
func runningDS4Windows() (exe, version string) {
	if exe = backend.DS4WindowsExePath(); exe != "" {
		version = ds4w.ExeVersion(exe)
	}
	return exe, version
}

// Close stops the Service and waits for its goroutine. A job that waits
// is dropped.
func (s *Service) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		started := s.started
		s.mu.Unlock()
		close(s.stop)
		if !started {
			close(s.done)
		}
	})
	<-s.done
}

// run ticks while a job waits, and on each wake.
func (s *Service) run(wake <-chan struct{}, unwatch func()) {
	defer close(s.done)
	defer unwatch()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		var tick <-chan time.Time
		if wait := s.safeTick(); wait > 0 {
			timer.Reset(wait)
			tick = timer.C
		}
		select {
		case <-s.stop:
			return
		case <-s.kick:
		case <-wake:
		case <-tick:
		}
		timer.Stop()
	}
}

// poke has the goroutine tick at once.
func (s *Service) poke() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// safeTick is tick, with a panic logged: the Service goes on, and EDSense
// with it.
func (s *Service) safeTick() (wait time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Install service: %v", r)
			wait = s.o.Every
		}
	}()
	return s.tick()
}

// tick does what is due, and says how long until it should tick again; 0:
// until it is woken.
func (s *Service) tick() time.Duration {
	var wait time.Duration
	soon := func(d time.Duration) {
		if d > 0 && (wait == 0 || d < wait) {
			wait = d
		}
	}
	soon(s.firstAdd())
	soon(s.sample())
	// a card published while a write ran says so: it is made again after
	if s.dsx.Waiting() {
		before := s.dsx.Job()
		s.dsx.Step()
		if s.dsx.Job() != before {
			s.refresh(control.AppDSX)
		} else {
			s.publish(control.AppDSX)
		}
		if s.dsx.Waiting() {
			soon(s.o.Every)
		}
	}
	if before := s.ds4.Job(); before.State == ds4w.JobWaiting {
		s.ds4.Step()
		after := s.ds4.Job()
		if after.State != before.State {
			s.ds4End(after)
			s.refresh(control.AppDS4Windows)
		} else {
			s.publish(control.AppDS4Windows)
		}
		if after.State == ds4w.JobWaiting {
			soon(s.o.Every)
		}
	}
	return wait
}

// sample looks for a DS4Windows that runs when it is time, and says how
// long until the next look.
func (s *Service) sample() time.Duration {
	now := s.o.Now()
	s.mu.Lock()
	next := s.seeNext
	s.mu.Unlock()
	if now.Before(next) {
		return next.Sub(now)
	}
	if _, _, ended := s.seeDS4(); ended {
		s.publish(control.AppDS4Windows)
	}
	s.mu.Lock()
	s.seeNext = now.Add(s.o.Sample)
	s.mu.Unlock()
	return s.o.Sample
}

// firstAdd runs DSX's first add once it is due: Delay after DSX became
// the app, while it is the app and FirstAdd allows it. It returns how long
// until it is due; 0 when it is not waited for.
func (s *Service) firstAdd() time.Duration {
	now := s.o.Now()
	onDSX := s.o.Kind() == control.AppDSX
	s.mu.Lock()
	if onDSX && !s.onDSX {
		s.dsxSince = now
	}
	s.onDSX = onDSX
	due := onDSX && !s.firstDone
	at := s.dsxSince.Add(s.o.Delay)
	if s.firstNext.After(at) {
		at = s.firstNext
	}
	s.mu.Unlock()
	if !due || !s.o.FirstAdd() {
		return 0
	}
	if now.Before(at) {
		return at.Sub(now)
	}
	if !s.dsx.FirstCheck() { // DSX's folder was not found: it looks again later
		s.mu.Lock()
		s.firstNext = now.Add(s.o.Delay)
		s.mu.Unlock()
		return s.o.Delay
	}
	s.mu.Lock()
	s.firstDone = true
	s.mu.Unlock()
	s.refresh(control.AppDSX)
	return 0
}

// ds4End tells the player how a DS4Windows install ended, wherever they
// are: the window may be closed by then.
func (s *Service) ds4End(j ds4w.Job) {
	var msg string
	switch j.State {
	case ds4w.JobDone:
		if msg = doneMessage(j.Result); msg == "" { // nothing was left to write
			s.mu.Lock()
			s.ds4Ended = s.ds4Asked
			s.mu.Unlock()
		}
	case ds4w.JobFailed:
		msg = "The DS4Windows profile for Elite could not be written:\n" + j.Err
		if w := wroteBefore(j.Result); w != "" {
			msg += "\n\n" + w
		}
	case ds4w.JobInterrupted:
		msg = "The DS4Windows profile for Elite is only partly written:\n" + j.Err +
			"\n\nExit DS4Windows and install the profile again on the Controller page."
	}
	if msg != "" && s.o.Notify != nil {
		s.o.Notify(msg)
	}
}

// State is app's profile card, from a look at its files that is at most a
// second old. It may read files: never on the loop.
func (s *Service) State(app string) control.ProfileState {
	st, _ := s.Look(app)
	return st
}

// Look is State, with DS4Windows' plan for the setup checklist (nil for
// DSX).
func (s *Service) Look(app string) (control.ProfileState, *ds4w.Plan) {
	switch app {
	case control.AppDSX:
		s.dsxLook.Lock()
		defer s.dsxLook.Unlock()
		s.mu.Lock()
		old := s.dsxSeen.at
		s.mu.Unlock()
		if old.IsZero() || s.o.Now().Sub(old) >= lookKeep {
			s.lookDSX()
		}
		return s.publish(app), nil
	case control.AppDS4Windows:
		s.ds4Look.Lock()
		defer s.ds4Look.Unlock()
		s.mu.Lock()
		old := s.ds4Seen.at
		s.mu.Unlock()
		if old.IsZero() || s.o.Now().Sub(old) >= lookKeep {
			s.lookDS4()
		}
		st := s.publish(app)
		s.mu.Lock()
		plan := s.ds4Seen.plan
		s.mu.Unlock()
		return st, &plan
	}
	return control.ProfileState{App: app, State: control.ProfileUnknown}, nil
}

// refresh looks at app's files again, and publishes what it finds.
func (s *Service) refresh(app string) {
	switch app {
	case control.AppDSX:
		s.dsxLook.Lock()
		defer s.dsxLook.Unlock()
		s.lookDSX()
	case control.AppDS4Windows:
		s.ds4Look.Lock()
		defer s.ds4Look.Unlock()
		s.lookDS4()
	}
	s.publish(app)
}

// Install asks for EDSense's profile for app to be written, or with reset
// written again over the one there, as soon as the app is closed. key is
// the key of the question the player answered: when the card asks
// another one now, nothing is asked (ErrChanged); "" asks nothing about
// it. The answer is the card after the request. It refuses a profile that
// cannot be written (the card says why), one that is there already, and
// while a write runs (ErrBusy).
func (s *Service) Install(app string, reset bool, key string) (control.ProfileState, error) {
	var err error
	switch app {
	case control.AppDSX:
		s.dsxLook.Lock()
		defer s.dsxLook.Unlock()
		if key != "" {
			s.lookDSX()
			err = s.asked(app, reset, key)
		}
		if err == nil {
			err = s.dsx.Request(reset)
		}
		switch {
		case err != nil:
			log.Printf("DSX profile not asked for: %v", err)
		case reset:
			log.Print("DSX profile reset requested")
		default:
			log.Print("DSX profile requested")
		}
		s.lookDSX()
	case control.AppDS4Windows:
		s.ds4Look.Lock()
		defer s.ds4Look.Unlock()
		l := s.lookDS4()
		var shown []string // the steps the question named
		if key != "" {
			err, shown = s.asked(app, reset, key), l.plan.Todo(reset)
		}
		if err == nil {
			err = s.ds4.RequestSteps(l.in, reset, shown)
		}
		if errors.Is(err, ds4w.ErrChanged) {
			err = ErrChanged
		}
		if err != nil {
			log.Printf("DS4Windows profile not asked for: %v", err)
		} else {
			s.mu.Lock()
			s.ds4Asked++
			s.mu.Unlock()
			what := "install"
			if reset {
				what = "reset"
			}
			log.Printf("DS4Windows profile %s requested, in %s", what, l.in.Dir)
		}
	default:
		return control.ProfileState{App: app, State: control.ProfileUnknown}, fmt.Errorf("no controller app %q", app)
	}
	st := s.publish(app)
	if err == nil {
		s.poke()
	}
	if errors.Is(err, dsx.ErrBusy) || errors.Is(err, ds4w.ErrBusy) {
		err = busyError{err}
	}
	return st, err
}

// asked: key is the key of the question the card asks now before app's
// install (or reset), from the last look; else ErrChanged.
func (s *Service) asked(app string, reset bool, key string) error {
	st := s.publish(app)
	q := st.Install
	if reset {
		q = st.Reset
	}
	if q == nil || q.Key != key {
		return ErrChanged
	}
	return nil
}

// Cancel ends app's job when it waits. It reads no file: the card is made
// from the last look.
func (s *Service) Cancel(app string) control.ProfileState {
	var ok bool
	switch app {
	case control.AppDSX:
		ok = s.dsx.Cancel()
	case control.AppDS4Windows:
		ok = s.ds4.Cancel()
	default:
		return control.ProfileState{App: app, State: control.ProfileUnknown}
	}
	if ok {
		log.Printf("%s profile: cancelled", backend.WordsFor(backend.Kind(app)).Name)
		s.poke()
	}
	return s.publish(app)
}

// Profiles are the cards published last, DSX's first; an app's is there
// once it was looked at.
func (s *Service) Profiles() []control.ProfileState {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []control.ProfileState
	for _, app := range []string{control.AppDSX, control.AppDS4Windows} {
		if st, ok := s.states[app]; ok {
			out = append(out, st)
		}
	}
	return out
}

// Watch wakes the returned channel whenever a card is published, until
// stop is called.
func (s *Service) Watch() (wake <-chan struct{}, stop func()) { return s.w.Add() }

// BackupDir is the folder of the copies EDSense keeps of app's files; ""
// for another app.
func (s *Service) BackupDir(app string) string {
	switch app {
	case control.AppDSX:
		return filepath.Join(s.o.DataDir, DSXBackups)
	case control.AppDS4Windows:
		return filepath.Join(s.o.DataDir, DS4WindowsBackups)
	}
	return ""
}

// publish makes app's card from the last look and the job now, and
// publishes it when it changed. It reads no file.
func (s *Service) publish(app string) control.ProfileState {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st control.ProfileState
	switch app {
	case control.AppDSX:
		st = dsxProfile(s.dsxSeen, s.dsx.Job())
	default:
		st = ds4Profile(s.ds4Seen, s.ds4.Job(), s.ds4Ended != s.ds4Asked)
	}
	old, ok := s.states[app]
	st.Rev = old.Rev
	if !ok || !reflect.DeepEqual(old, st) {
		s.rev++
		st.Rev = s.rev
		s.states[app] = st
		s.w.Wake()
	}
	return st
}

// firstPending: the first add may still add DSX's profile by itself in
// this run.
func (s *Service) firstPending() bool {
	if s.o.Kind() != control.AppDSX || !s.o.FirstAdd() || s.dsx.FirstChecked() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.firstDone
}

// lookDSX reads DSX's files; under dsxLook.
func (s *Service) lookDSX() {
	l := dsxSeen{look: s.dsx.Look(), backups: isDir(s.BackupDir(control.AppDSX)), autoAdd: s.firstPending(), at: s.o.Now()}
	if l.look.Folder != "" {
		l.shown = ds4w.ShowDir(l.look.Folder, s.o.Env("APPDATA"), s.o.Env("USERPROFILE"))
	}
	s.mu.Lock()
	s.dsxSeen = l
	s.mu.Unlock()
}

// lookDS4 reads DS4Windows' files, in the folder of the DS4Windows that
// runs, else in the one seen while it ran, else in %APPDATA%; under
// ds4Look.
func (s *Service) lookDS4() ds4Seen {
	exe, version, _ := s.seeDS4()
	appData := s.o.Env("APPDATA")
	s.mu.Lock()
	dir, also, exeDir := s.ds4Dir, s.ds4Also, s.ds4ExeDir
	s.mu.Unlock()
	if dir == "" {
		dir = ds4w.DataDir("", appData)
	}
	in := ds4w.Input{Dir: dir, ExeDir: exeDir, Elite: s.o.Elite(), Version: version, Also: also}
	if s.o.Kind() == control.AppDS4Windows && s.o.Report != nil {
		if r := s.o.Report(); r != nil {
			in.Slot = r.Slot
			if !r.ByRule() {
				in.Usual = r.Profile
			}
		}
	}
	l := ds4Seen{in: in, plan: ds4w.Inspect(in), running: exe != "", backups: isDir(s.BackupDir(control.AppDS4Windows)),
		port: s.o.Port(), at: s.o.Now(), appData: appData, home: s.o.Env("USERPROFILE")}
	l.shown = l.showDir(dir)
	s.mu.Lock()
	s.ds4Seen = l
	s.mu.Unlock()
	return l
}

// seeDS4 looks for a DS4Windows that runs, and keeps its folders: an
// install writes where DS4Windows was seen, though it is closed by then
// (a portable DS4Windows keeps its settings next to its exe). ended: a
// job done is seen through now.
func (s *Service) seeDS4() (exe, version string, ended bool) {
	if exe, version = s.o.DS4Exe(); exe == "" {
		return "", "", false
	}
	exeDir := filepath.Dir(exe)
	dir, also := ds4w.DataDirs(exeDir, s.o.Env("APPDATA"))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ds4Dir, s.ds4Also, s.ds4ExeDir = dir, also, exeDir
	if s.ds4.Job().State == ds4w.JobDone && s.ds4Ended != s.ds4Asked {
		s.ds4Ended, ended = s.ds4Asked, true // DS4Windows runs again: what was written is simply there
	}
	return exe, version, ended
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
