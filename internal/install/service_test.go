package install

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
)

// Every test here works in temporary folders: a fake %APPDATA% with a
// DS4Windows data folder, a fake DSX folder, and EDSense's data folder.
// None looks at a running app, the real %APPDATA% or a real install.

const settingsOff = `<?xml version="1.0" encoding="utf-8"?>
<Profile app_version="5.0.12.0" config_version="5">
  <Controller1>Default</Controller1>
  <UseDSXUDPServer>False</UseDSXUDPServer>
  <DSXUDPServerPort>6969</DSXUDPServerPort>
  <DSXUDPServerListenAddress>127.0.0.1</DSXUDPServerListenAddress>
</Profile>
`

const autoEmpty = `<?xml version="1.0" encoding="utf-8"?>
<Programs>
</Programs>
`

// ds4Folder makes a DS4Windows data folder in dir.
func ds4Folder(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "Profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"Profiles.xml": settingsOff, "Auto Profiles.xml": autoEmpty} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.ReplaceAll(text, "\n", "\r\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// rig is a Service on fakes.
type rig struct {
	t        *testing.T
	s        *Service
	data     string // EDSense's
	appData  string
	dsxDir   string
	kind     atomic.Value // string
	firstAdd atomic.Bool
	wake     chan struct{}

	mu        sync.Mutex
	told      []string
	dsxRuns   bool
	dsxFound  bool
	ds4Exe    string
	ds4Closed bool
	logs      *bytes.Buffer
	looks     int  // at the system: DSX's folder and the DS4Windows that runs
	manual    bool // made with Make: the test starts it
}

func newRig(t *testing.T, kind string, edits ...func(r *rig, o *Options)) *rig {
	t.Helper()
	r := &rig{t: t, data: t.TempDir(), appData: t.TempDir(), dsxDir: t.TempDir(), wake: make(chan struct{}, 1), dsxFound: true}
	r.kind.Store(kind)
	ds4Folder(t, filepath.Join(r.appData, "DS4Windows"))
	for _, d := range []string{"Controller Profiles", "Game Profiles"} {
		if err := os.MkdirAll(filepath.Join(r.dsxDir, "DSX_Savefile", "Configuration Files", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.logs = &bytes.Buffer{}
	log.SetOutput(lockedWriter{&r.mu, r.logs})
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	notify := func(msg string) {
		r.mu.Lock()
		r.told = append(r.told, msg)
		r.mu.Unlock()
	}
	o := Options{
		DataDir:  r.data,
		Notify:   notify,
		Kind:     func() string { return r.kind.Load().(string) },
		FirstAdd: r.firstAdd.Load,
		Watch:    func() (<-chan struct{}, func()) { return r.wake, func() {} },
		Port:     func() int { return 0 },
		DSX: dsx.NewInstaller(dsx.InstallerOptions{
			Backups: filepath.Join(r.data, DSXBackups),
			Notify:  notify,
			Running: func() bool {
				r.mu.Lock()
				defer r.mu.Unlock()
				return r.dsxRuns
			},
			Folder: func(bool) string {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.looks++
				if r.dsxFound {
					return r.dsxDir
				}
				return ""
			},
			Elite: func() string {
				return `C:\Games\Elite Dangerous\Products\elite-dangerous-odyssey-64\EliteDangerous64.exe`
			},
		}),
		DS4: ds4w.NewInstaller(ds4w.InstallerOptions{
			Backups: filepath.Join(r.data, DS4WindowsBackups),
			Closed: func(string) (bool, string) {
				r.mu.Lock()
				defer r.mu.Unlock()
				return r.ds4Closed, "a test says it runs"
			},
			Hold: 30 * time.Millisecond,
		}),
		DS4Exe: func() (string, string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.looks++
			if r.ds4Exe == "" {
				return "", ""
			}
			return r.ds4Exe, "5.0.12.0"
		},
		Elite: func() []string { return nil },
		Env: func(key string) string {
			return map[string]string{"APPDATA": r.appData, "USERPROFILE": filepath.Join(r.appData, "home")}[key]
		},
		Every:  10 * time.Millisecond,
		Delay:  40 * time.Millisecond,
		Sample: time.Hour,
	}
	for _, e := range edits {
		e(r, &o)
	}
	if r.manual {
		r.s = Make(o)
	} else {
		r.s = New(o)
	}
	t.Cleanup(r.s.Close)
	return r
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (r *rig) set(f func(r *rig)) {
	r.mu.Lock()
	f(r)
	r.mu.Unlock()
}

func (r *rig) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *rig) take() (told []string, logs string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	told, r.told = r.told, nil
	logs = r.logs.String()
	r.logs.Reset()
	return told, logs
}

// eventually waits up to 2 s for ok.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("not within 2 s: %s", what)
		}
	}
}

// published is app's card as Profiles has it.
func (r *rig) published(app string) control.ProfileState {
	for _, st := range r.s.Profiles() {
		if st.App == app {
			return st
		}
	}
	return control.ProfileState{}
}

// TestServiceDS4Windows: an install waits while DS4Windows runs, writes
// once it has been closed for a while, tells the player, and shows done
// until DS4Windows runs again. Each change is published.
func TestServiceDS4Windows(t *testing.T) {
	r := newRig(t, control.AppDS4Windows)
	wake, stop := r.s.Watch()
	defer stop()

	st := r.s.State(control.AppDS4Windows)
	if st.State != control.ProfileMissing || !st.CanInstall || st.CanReset || st.CanCancel || st.Dir != `%APPDATA%\DS4Windows` ||
		len(st.Steps) != 3 || len(st.Files) != 3 || st.Install == nil || !strings.Contains(st.Install.Text, `%APPDATA%\DS4Windows`) {
		t.Fatalf("missing: %+v", st)
	}
	select {
	case <-wake:
	default:
		t.Fatal("the first card was not published")
	}
	if again := r.s.State(control.AppDS4Windows); again.Rev != st.Rev {
		t.Errorf("an unchanged card was published again: rev %d, then %d", st.Rev, again.Rev)
	}

	st, err := r.s.Install(control.AppDS4Windows, false, "")
	if err != nil || st.State != control.ProfileWaiting || !st.CanCancel || st.CanInstall || st.Install != nil {
		t.Fatalf("install: %+v %v", st, err)
	}
	time.Sleep(100 * time.Millisecond) // ticks while DS4Windows "runs"
	if got := r.published(control.AppDS4Windows); got.State != control.ProfileWaiting {
		t.Fatalf("written while DS4Windows runs: %+v", got)
	}
	_, logs := r.take()
	if strings.Count(logs, "DS4Windows profile: waiting for DS4Windows to be closed") != 1 ||
		!strings.Contains(logs, "DS4Windows profile install requested, in "+filepath.Join(r.appData, "DS4Windows")) {
		t.Errorf("log %q", logs)
	}

	r.set(func(r *rig) { r.ds4Closed = true })
	eventually(t, "written", func() bool { return r.published(control.AppDS4Windows).State == control.ProfileDone })
	told, _ := r.take()
	if !slices.Equal(told, []string{"The DS4Windows profile for Elite is written. Start DS4Windows again to use it."}) {
		t.Errorf("told %q", told)
	}
	if _, err := os.Stat(filepath.Join(r.appData, "DS4Windows", "Profiles", ds4w.ProfileName+".xml")); err != nil {
		t.Fatal(err)
	}
	if st := r.s.State(control.AppDS4Windows); st.State != control.ProfileDone || !st.Backups {
		t.Errorf("done: %+v", st)
	}

	// DS4Windows runs again (installed, so its settings are in %APPDATA%):
	// what was written is simply there
	r.set(func(r *rig) { r.ds4Exe = filepath.Join(t.TempDir(), "DS4Windows.exe") })
	time.Sleep(lookKeep)
	st = r.s.State(control.AppDS4Windows)
	if st.State != control.ProfileOurs || !st.CanReset || st.Reset == nil || !st.Reset.Danger || st.CanInstall {
		t.Fatalf("ours: %+v", st)
	}

	// a reset that waits can be cancelled
	r.set(func(r *rig) { r.ds4Closed = false })
	if st, err := r.s.Install(control.AppDS4Windows, true, ""); err != nil || st.State != control.ProfileWaiting {
		t.Fatalf("reset: %+v %v", st, err)
	}
	r.mu.Lock()
	looks := r.looks
	r.mu.Unlock()
	if st := r.s.Cancel(control.AppDS4Windows); st.State != control.ProfileOurs || st.CanCancel {
		t.Fatalf("cancelled: %+v", st)
	}
	r.mu.Lock()
	if r.looks != looks {
		t.Errorf("a cancel looked at the system %d times", r.looks-looks)
	}
	r.mu.Unlock()
	if _, logs := r.take(); !strings.Contains(logs, "DS4Windows profile: cancelled") {
		t.Errorf("log %q", logs)
	}
	// nothing to install
	if _, err := r.s.Install(control.AppDS4Windows, false, ""); !errors.Is(err, ds4w.ErrNothing) || errors.Is(err, ErrBusy) {
		t.Errorf("install over ours: %v", err)
	}
}

// TestServiceDataFolder: the data folder of the DS4Windows seen running is
// kept after it closes, and the card names it; a portable folder outside
// the user's folders is shown as it is.
func TestServiceDataFolder(t *testing.T) {
	r := newRig(t, control.AppDSX)
	if err := os.RemoveAll(filepath.Join(r.appData, "DS4Windows")); err != nil {
		t.Fatal(err)
	}
	portable := filepath.Join(t.TempDir(), "Tools", "DS4Windows")
	ds4Folder(t, portable)
	r.set(func(r *rig) { r.ds4Exe = filepath.Join(portable, "DS4Windows.exe") })
	if st := r.s.State(control.AppDS4Windows); st.Dir != portable {
		t.Fatalf("running portable: %q", st.Dir)
	}
	r.set(func(r *rig) { r.ds4Exe = ""; r.ds4Closed = true })
	time.Sleep(lookKeep)
	if st := r.s.State(control.AppDS4Windows); st.Dir != portable {
		t.Fatalf("closed since: %q", st.Dir)
	}
	if _, err := r.s.Install(control.AppDS4Windows, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "written", func() bool { return r.published(control.AppDS4Windows).State == control.ProfileDone })
	if _, err := os.Stat(filepath.Join(portable, "Profiles", ds4w.ProfileName+".xml")); err != nil {
		t.Fatalf("not written in the portable folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.appData, "DS4Windows", "Profiles", ds4w.ProfileName+".xml")); err == nil {
		t.Fatal("written in %APPDATA% too")
	}
}

// TestServiceSamples: a DS4Windows seen running only between two looks of
// the page is seen all the same, and an install writes into its folder.
func TestServiceSamples(t *testing.T) {
	portable := filepath.Join(t.TempDir(), "Tools", "DS4Windows")
	ds4Folder(t, portable)
	looks := atomic.Int32{}
	r := newRig(t, control.AppDS4Windows, func(r *rig, o *Options) {
		o.Sample = 20 * time.Millisecond
		exe := o.DS4Exe
		o.DS4Exe = func() (string, string) {
			looks.Add(1)
			return exe()
		}
	})
	if err := os.RemoveAll(filepath.Join(r.appData, "DS4Windows")); err != nil {
		t.Fatal(err)
	}
	r.set(func(r *rig) { r.ds4Exe = filepath.Join(portable, "DS4Windows.exe") })
	eventually(t, "looks", func() bool { return looks.Load() >= 3 })
	r.set(func(r *rig) { r.ds4Exe = "" })
	if st := r.s.State(control.AppDS4Windows); st.Dir != portable || st.State != control.ProfileMissing {
		t.Fatalf("seen running before: %+v", st)
	}
}

// TestServiceFirstAdd: DSX's first add waits until DSX is the app and
// FirstAdd allows it (the first run is over, DSX chosen or surely
// running), then for Delay; it looks again for a folder it did not find,
// and runs once.
func TestServiceFirstAdd(t *testing.T) {
	r := newRig(t, control.AppDS4Windows)
	r.firstAdd.Store(true)
	r.poke()
	time.Sleep(100 * time.Millisecond)
	if j := r.s.dsx.Job(); j.State != "" {
		t.Fatalf("added while DS4Windows is the app: %+v", j)
	}

	r.kind.Store(control.AppDSX)
	r.firstAdd.Store(false)
	r.set(func(r *rig) { r.dsxFound = false; r.dsxRuns = true })
	r.poke()
	time.Sleep(100 * time.Millisecond)
	if j := r.s.dsx.Job(); j.State != "" {
		t.Fatalf("added while FirstAdd says no: %+v", j)
	}
	if _, logs := r.take(); logs != "" {
		t.Fatalf("log %q", logs)
	}

	r.firstAdd.Store(true)
	r.poke()
	time.Sleep(150 * time.Millisecond) // tried, and tried again
	_, logs := r.take()
	if strings.Count(logs, "DSX folder not found") != 1 {
		t.Fatalf("no folder: log %q", logs)
	}
	r.set(func(r *rig) { r.dsxFound = true })
	eventually(t, "the first add", func() bool { return r.s.dsx.Job().State == dsx.JobWaiting })
	eventually(t, "published", func() bool { return r.published(control.AppDSX).State == control.ProfileWaiting })
	told, _ := r.take()
	if len(told) != 1 || !strings.Contains(told[0], "It will be added the next time DSX is closed") {
		t.Fatalf("told %q", told)
	}

	r.set(func(r *rig) { r.dsxRuns = false })
	eventually(t, "written", func() bool { return r.published(control.AppDSX).State == control.ProfileDone })
	if told, _ := r.take(); len(told) != 1 || told[0] != "The \"Elite Dangerous\" controller profile is now in DSX. Start DSX again to use it." {
		t.Fatalf("told %q", told)
	}

	// once only, also after a switch away and back
	if err := os.Remove(filepath.Join(r.dsxDir, "DSX_Savefile", "Configuration Files", "Controller Profiles", "Elite Dangerous.dsx")); err != nil {
		t.Fatal(err)
	}
	r.kind.Store(control.AppDS4Windows)
	r.poke()
	time.Sleep(30 * time.Millisecond)
	r.kind.Store(control.AppDSX)
	r.poke()
	time.Sleep(100 * time.Millisecond)
	if j := r.s.dsx.Job(); j.State == dsx.JobWaiting {
		t.Fatalf("a second first add: %+v", j)
	}
}

// TestServiceDSX: an install and a reset the player asks for, which
// outlive a switch to DS4Windows; a cancel; and the questions asked first.
func TestServiceDSX(t *testing.T) {
	r := newRig(t, control.AppDS4Windows)
	st := r.s.State(control.AppDSX)
	if st.State != control.ProfileMissing || !st.CanInstall || st.CanReset || st.Install == nil || st.Reset != nil ||
		len(st.Files) != 2 || st.Dir != r.dsxDir {
		t.Fatalf("missing: %+v", st)
	}
	r.set(func(r *rig) { r.dsxRuns = true })
	if st, err := r.s.Install(control.AppDSX, false, ""); err != nil || st.State != control.ProfileWaiting || !st.CanCancel {
		t.Fatalf("install: %+v %v", st, err)
	}
	if st := r.s.Cancel(control.AppDSX); st.State != control.ProfileMissing || st.CanCancel {
		t.Fatalf("cancelled: %+v", st)
	}
	if _, err := r.s.Install(control.AppDSX, false, ""); err != nil {
		t.Fatal(err)
	}
	r.set(func(r *rig) { r.dsxRuns = false })
	eventually(t, "written", func() bool { return r.published(control.AppDSX).State == control.ProfileDone })
	r.set(func(r *rig) { r.dsxRuns = true })
	time.Sleep(lookKeep)
	st = r.s.State(control.AppDSX)
	if st.State != control.ProfilePresent || !st.CanReset || st.Reset == nil || st.Reset.Text != DSXResetQuestion || st.Reset.OK != "Reset" {
		t.Fatalf("present: %+v", st)
	}
	if _, err := r.s.Install(control.AppDSX, true, ""); err != nil {
		t.Fatal(err)
	}
	_, logs := r.take()
	if !strings.Contains(logs, "DSX profile reset requested") {
		t.Errorf("log %q", logs)
	}
	r.set(func(r *rig) { r.dsxRuns = false })
	eventually(t, "reset", func() bool { return r.published(control.AppDSX).State == control.ProfileDone })
	if copies, _ := filepath.Glob(filepath.Join(r.data, DSXBackups, "*.dsx")); len(copies) != 1 {
		t.Errorf("copies %v", copies)
	}
	if !r.s.State(control.AppDSX).Backups {
		t.Error("no backups to open")
	}
	if got := r.s.BackupDir(control.AppDSX); got != filepath.Join(r.data, DSXBackups) {
		t.Errorf("backups %q", got)
	}
	// a folder no longer found, and gone
	r.set(func(r *rig) { r.dsxFound = false })
	if err := os.RemoveAll(filepath.Join(r.dsxDir, "DSX_Savefile")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.Install(control.AppDSX, true, ""); !errors.Is(err, dsx.ErrNoFolder) {
		t.Errorf("no folder: %v", err)
	}
}

// TestServiceTwoFolders: settings next to DS4Windows' exe and in
// %APPDATA% both: DS4Windows asks at each start which to use, so EDSense
// writes in neither, and the card names both.
func TestServiceTwoFolders(t *testing.T) {
	r := newRig(t, control.AppDS4Windows)
	portable := filepath.Join(t.TempDir(), "Tools", "DS4Windows")
	ds4Folder(t, portable)
	r.set(func(r *rig) { r.ds4Exe = filepath.Join(portable, "DS4Windows.exe") })
	st := r.s.State(control.AppDS4Windows)
	if st.State != control.ProfileBlocked || st.Block != ds4w.BlockTwo || st.CanInstall ||
		!strings.Contains(st.Text, portable) || !strings.Contains(st.Text, `%APPDATA%\DS4Windows`) {
		t.Fatalf("two folders: %+v", st)
	}
	var be *ds4w.BlockedError
	if _, err := r.s.Install(control.AppDS4Windows, false, ""); !errors.As(err, &be) {
		t.Errorf("install: %v", err)
	}
}

// TestServiceKey: an install is asked for only with the key of the
// question the card asks now: not after the folder or the steps changed
// since the question was shown. Without a key nothing is compared.
func TestServiceKey(t *testing.T) {
	r := newRig(t, control.AppDS4Windows)
	if err := os.RemoveAll(filepath.Join(r.appData, "DS4Windows")); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(r.appData, "DS4Windows")
	ds4Folder(t, installed)
	st := r.s.State(control.AppDS4Windows)
	if st.Install == nil || st.Install.Key == "" {
		t.Fatalf("no key: %+v", st)
	}
	asked := st.Install.Key

	// the player turns game mod support on in DS4Windows meanwhile: the
	// question would list two files now
	on := strings.Replace(settingsOff, "<UseDSXUDPServer>False", "<UseDSXUDPServer>True", 1)
	if err := os.WriteFile(filepath.Join(installed, "Profiles.xml"), []byte(on), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(lookKeep)
	if _, err := r.s.Install(control.AppDS4Windows, false, asked); !errors.Is(err, ErrChanged) || r.s.ds4.Job().State != "" {
		t.Fatalf("steps changed: %v %+v", err, r.s.ds4.Job())
	}
	st = r.s.State(control.AppDS4Windows)
	if st.Install.Key == asked {
		t.Fatalf("the same key: %q", asked)
	}
	// a portable DS4Windows seen since: another folder
	portable := filepath.Join(t.TempDir(), "Tools", "DS4Windows")
	ds4Folder(t, portable)
	if err := os.RemoveAll(installed); err != nil {
		t.Fatal(err)
	}
	asked = st.Install.Key
	r.set(func(r *rig) { r.ds4Exe = filepath.Join(portable, "DS4Windows.exe") })
	if _, err := r.s.Install(control.AppDS4Windows, false, asked); !errors.Is(err, ErrChanged) || r.s.ds4.Job().State != "" {
		t.Fatalf("folder changed: %v %+v", err, r.s.ds4.Job())
	}
	if _, logs := r.take(); !strings.Contains(logs, "DS4Windows profile not asked for: what it would write changed") {
		t.Errorf("log %q", logs)
	}
	// the question shown now: asked, with its steps
	st = r.s.State(control.AppDS4Windows)
	if st, err := r.s.Install(control.AppDS4Windows, false, st.Install.Key); err != nil || st.State != control.ProfileWaiting ||
		!slices.Equal(st.Files, []string{`Profiles\Elite Dangerous (EDSense).xml`, "Auto Profiles.xml", "Profiles.xml"}) {
		t.Fatalf("asked: %+v %v", st, err)
	}
	if j := r.s.ds4.Job(); j.Dir != portable || len(j.Steps) != 3 {
		t.Errorf("job %+v", j)
	}

	// DSX: a reset asked for over a game profile for Elite that changed
	st = r.s.State(control.AppDSX)
	if st.Install == nil {
		t.Fatalf("dsx: %+v", st)
	}
	if _, err := r.s.Install(control.AppDSX, true, st.Install.Key); !errors.Is(err, ErrChanged) {
		t.Errorf("dsx: the add's key for a reset: %v", err)
	}
	if _, err := r.s.Install(control.AppDSX, false, st.Install.Key); err != nil || r.s.dsx.Job().State != dsx.JobWaiting {
		t.Errorf("dsx add: %v %+v", err, r.s.dsx.Job())
	}
}

// TestServiceFirstAddText: the DSX card says the profile is added when
// DSX is closed only while the first add may still do it; after the
// player cancelled, or without FirstAdd, Install... is the way.
func TestServiceFirstAddText(t *testing.T) {
	const auto, byHand = "It is added when DSX is closed.", "Install... adds it."
	r := newRig(t, control.AppDSX, func(r *rig, o *Options) { o.Delay = time.Hour })
	if st := r.s.State(control.AppDSX); !strings.HasSuffix(st.Text, byHand) {
		t.Fatalf("no first add: %q", st.Text)
	}
	r.firstAdd.Store(true)
	time.Sleep(lookKeep)
	if st := r.s.State(control.AppDSX); !strings.HasSuffix(st.Text, auto) {
		t.Fatalf("first add to come: %q", st.Text)
	}
	r.set(func(r *rig) { r.dsxRuns = true }) // the job waits, so Cancel finds it waiting
	if _, err := r.s.Install(control.AppDSX, false, ""); err != nil {
		t.Fatal(err)
	}
	if st := r.s.Cancel(control.AppDSX); st.State != control.ProfileMissing {
		t.Fatalf("cancelled: %+v", st)
	}
	time.Sleep(lookKeep)
	if st := r.s.State(control.AppDSX); !strings.HasSuffix(st.Text, byHand) {
		t.Fatalf("after a cancel: %q", st.Text)
	}
}

// TestServicePlayerFirst: a DSX profile the player asked for and then
// cancelled is not added by the first add once the first run is over.
func TestServicePlayerFirst(t *testing.T) {
	r := newRig(t, control.AppDSX)
	r.set(func(r *rig) { r.dsxRuns = true })
	if _, err := r.s.Install(control.AppDSX, false, ""); err != nil {
		t.Fatal(err)
	}
	if st := r.s.Cancel(control.AppDSX); st.State != control.ProfileMissing {
		t.Fatalf("cancelled: %+v", st)
	}
	r.firstAdd.Store(true)
	r.poke()
	time.Sleep(150 * time.Millisecond)
	if j := r.s.dsx.Job(); j.State != "" {
		t.Fatalf("added after the player cancelled: %+v", j)
	}
}

// TestServiceStart: Make does not start the goroutine (a second EDSense
// that has not got the instance yet writes nothing); its calls work, and
// Close needs no Start.
func TestServiceStart(t *testing.T) {
	base := runtime.NumGoroutine()
	r := newRig(t, control.AppDSX, func(r *rig, o *Options) { r.manual = true })
	r.set(func(r *rig) { r.dsxRuns = true })
	r.firstAdd.Store(true)
	r.poke()
	time.Sleep(100 * time.Millisecond)
	if j := r.s.dsx.Job(); j.State != "" {
		t.Fatalf("the first add ran before Start: %+v", j)
	}
	if st := r.s.State(control.AppDSX); st.State != control.ProfileMissing {
		t.Fatalf("state %+v", st)
	}
	r.s.Start()
	eventually(t, "the first add", func() bool { return r.s.dsx.Job().State == dsx.JobWaiting })
	r.s.Close()
	r.s.Start() // after Close: nothing
	q := newRig(t, control.AppDSX, func(r *rig, o *Options) { r.manual = true })
	q.s.Close()
	q.s.Close()
	for end := time.Now().Add(2 * time.Second); runtime.NumGoroutine() > base; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("%d goroutines, %d before", runtime.NumGoroutine(), base)
		}
	}
}

// TestServicePanic: a panic in a tick is logged, and the Service goes on.
func TestServicePanic(t *testing.T) {
	var calls atomic.Int32
	r := newRig(t, control.AppDS4Windows, func(r *rig, o *Options) {
		kind := o.Kind
		o.Kind = func() string {
			if calls.Add(1) == 1 {
				panic("a bug")
			}
			return kind()
		}
	})
	eventually(t, "logged", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return strings.Contains(r.logs.String(), "Install service: a bug")
	})
	if _, err := r.s.Install(control.AppDS4Windows, false, ""); err != nil {
		t.Fatal(err)
	}
	r.set(func(r *rig) { r.ds4Closed = true })
	eventually(t, "written", func() bool { return r.published(control.AppDS4Windows).State == control.ProfileDone })
}

// TestServiceBusy: a request while a write runs is ErrBusy.
func TestServiceBusy(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r := newRig(t, control.AppDSX, func(r *rig, o *Options) {
		o.DSX = dsx.NewInstaller(dsx.InstallerOptions{
			Backups: filepath.Join(r.data, DSXBackups),
			Running: func() bool { return false },
			Folder:  func(bool) string { return r.dsxDir },
			Elite: func() string {
				once.Do(func() { close(started) })
				<-release
				return ""
			},
		})
	})
	if _, err := r.s.Install(control.AppDSX, false, ""); err != nil {
		t.Fatal(err)
	}
	<-started
	_, err := r.s.Install(control.AppDSX, true, "")
	close(release)
	if !errors.Is(err, ErrBusy) || !errors.Is(err, dsx.ErrBusy) {
		t.Fatalf("while writing: %v", err)
	}
}

// TestServiceClose: Close ends the goroutine, at once and more than once,
// while a job waits too.
func TestServiceClose(t *testing.T) {
	base := runtime.NumGoroutine()
	for range 10 {
		r := newRig(t, control.AppDSX)
		r.set(func(r *rig) { r.dsxRuns = true })
		if _, err := r.s.Install(control.AppDSX, false, ""); err != nil {
			t.Fatal(err)
		}
		r.s.Close()
		r.s.Close()
	}
	for end := time.Now().Add(2 * time.Second); runtime.NumGoroutine() > base; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("%d goroutines, %d before", runtime.NumGoroutine(), base)
		}
	}
}

// TestServiceIdle: with no job and no first add due, it ticks only to
// look for a DS4Windows that runs.
func TestServiceIdle(t *testing.T) {
	r := newRig(t, control.AppDS4Windows)
	if wait := r.s.tick(); wait < 59*time.Minute || wait > time.Hour {
		t.Fatalf("idle tick in %v", wait)
	}
	r.kind.Store(control.AppDSX)
	r.firstAdd.Store(true)
	if wait := r.s.tick(); wait <= 0 || wait > r.s.o.Delay {
		t.Fatalf("first add due in %v", wait)
	}
}

// TestServiceWriting: while a write runs, the card says so and offers no
// Cancel; a cancel then cancels nothing, and the card after it says what
// happens.
func TestServiceWriting(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var r *rig
	r = newRig(t, control.AppDS4Windows, func(rr *rig, o *Options) {
		o.DS4 = ds4w.NewInstaller(ds4w.InstallerOptions{
			Backups: filepath.Join(rr.data, DS4WindowsBackups),
			Closed: func(string) (bool, string) {
				if r != nil && r.s.ds4.Job().Writing {
					once.Do(func() {
						close(entered)
						<-release
					})
				}
				return true, ""
			},
			Hold: 30 * time.Millisecond,
		})
	})
	if _, err := r.s.Install(control.AppDS4Windows, false, ""); err != nil {
		t.Fatal(err)
	}
	<-entered
	st := r.s.State(control.AppDS4Windows)
	if st.State != control.ProfileWriting || st.CanCancel || st.Text != "Writing DS4Windows' settings now." {
		t.Errorf("writing: %+v", st)
	}
	if st := r.s.Cancel(control.AppDS4Windows); st.State != control.ProfileWriting {
		t.Errorf("cancel while writing: %+v", st)
	}
	close(release)
	eventually(t, "written", func() bool { return r.published(control.AppDS4Windows).State == control.ProfileDone })
}

// TestServiceKeyRace: DS4Windows' files change between the look the key
// is checked against and the request: the steps asked for are pinned, so
// nothing is asked.
func TestServiceKeyRace(t *testing.T) {
	var change atomic.Bool
	r := newRig(t, control.AppDS4Windows, func(rr *rig, o *Options) {
		o.Port = func() int {
			if change.Load() {
				on := strings.Replace(settingsOff, "<UseDSXUDPServer>False", "<UseDSXUDPServer>True", 1)
				if err := os.WriteFile(filepath.Join(rr.appData, "DS4Windows", "Profiles.xml"), []byte(on), 0o644); err != nil {
					t.Error(err)
				}
			}
			return 0
		}
	})
	st := r.s.State(control.AppDS4Windows)
	change.Store(true) // right after the next look
	if _, err := r.s.Install(control.AppDS4Windows, false, st.Install.Key); !errors.Is(err, ErrChanged) || r.s.ds4.Job().State != "" {
		t.Fatalf("changed after the look: %v %+v", err, r.s.ds4.Job())
	}
}
