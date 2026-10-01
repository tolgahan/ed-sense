package dsx

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/assets"
	"github.com/tolgahan/ed-sense/internal/elite"
)

// fakeDSX is a DSX folder in a temp dir, and the Installer's view of the
// system: whether DSX runs, whether its folder is found, and what the
// player was told.
type fakeDSX struct {
	t       *testing.T
	dir     string // DSX's folder
	backups string
	exe     string // Elite's

	mu      sync.Mutex
	running bool
	found   bool
	told    []string
	logs    *bytes.Buffer
}

func newFakeDSX(t *testing.T) *fakeDSX {
	t.Helper()
	f := &fakeDSX{t: t, dir: t.TempDir(), backups: filepath.Join(t.TempDir(), "dsx_profile_backups"), found: true,
		exe: `D:\SteamLibrary\steamapps\common\Elite Dangerous\Products\elite-dangerous-odyssey-64\EliteDangerous64.exe`}
	for _, d := range []string{filepath.Dir(profilePath(f.dir)), filepath.Dir(gameProfilesPath(f.dir))} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.logs = &bytes.Buffer{}
	log.SetOutput(f.logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	return f
}

func (f *fakeDSX) installer() *Installer {
	return NewInstaller(InstallerOptions{
		Backups: f.backups,
		Notify: func(msg string) {
			f.mu.Lock()
			f.told = append(f.told, msg)
			f.mu.Unlock()
		},
		Running: func() bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.running
		},
		Folder: func(bool) string {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.found {
				return f.dir
			}
			return ""
		},
		Elite: func() string { return f.exe },
		Now:   func() time.Time { return time.Date(2026, 10, 1, 12, 30, 45, 0, time.Local) },
	})
}

func (f *fakeDSX) set(running, found bool) {
	f.mu.Lock()
	f.running, f.found = running, found
	f.mu.Unlock()
}

// take is what the player was told, and the log, since the last take.
func (f *fakeDSX) take() (told []string, logs string) {
	f.mu.Lock()
	told, f.told = f.told, nil
	f.mu.Unlock()
	logs = f.logs.String()
	f.logs.Reset()
	return told, logs
}

func (f *fakeDSX) write(path, text string) {
	f.t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeDSX) read(path string) string {
	f.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func wantState(t *testing.T, i *Installer, state, msg string) {
	t.Helper()
	if s := i.State(); s.State != state || s.Msg != msg {
		t.Fatalf("state %q (%q), want %q (%q)", s.State, s.Msg, state, msg)
	}
}

// TestInstallerFirstCheck: the first add waits for DSX's folder and tries
// again until it is found, says once that it was not found, and adds the
// profile only when DSX has none, as soon as DSX is closed.
func TestInstallerFirstCheck(t *testing.T) {
	f := newFakeDSX(t)
	i := f.installer()
	f.set(false, false)
	wantState(t, i, StateNoFolder, "")
	if err := i.Request(false); !errors.Is(err, ErrNoFolder) {
		t.Fatalf("request without a folder: %v", err)
	}
	for range 2 {
		if i.FirstCheck() {
			t.Fatal("the first check is done without DSX's folder")
		}
	}
	if told, logs := f.take(); len(told) != 0 || strings.Count(logs, "\n") != 1 ||
		logs != "DSX folder not found: EDSense's Elite controller profile was not added (the Controller page tries again)\n" {
		t.Fatalf("no folder: told %q, log %q", told, logs)
	}

	// the folder appears while DSX runs: the player is told, once
	f.set(true, true)
	if !i.FirstCheck() {
		t.Fatal("the first check is not done with DSX's folder")
	}
	told, logs := f.take()
	if len(told) != 1 || !strings.Contains(told[0], "It will be added the next time DSX is closed (DSX tray icon > Exit)") ||
		logs != "DSX has no \"Elite Dangerous\" profile yet: EDSense adds its own when DSX is closed\n" {
		t.Fatalf("first add: told %q, log %q", told, logs)
	}
	wantState(t, i, StateWaiting, "")
	if j := i.Job(); !j.Auto || j.Reset {
		t.Fatalf("first add job %+v", j)
	}
	i.Step() // DSX runs: the player was told already, so no waiting line
	if told, logs := f.take(); len(told) != 0 || logs != "" {
		t.Fatalf("waiting: told %q, log %q", told, logs)
	}
	if i.FirstCheck() != true || i.Job().State != JobWaiting {
		t.Fatal("a second first check changed the job")
	}

	f.set(false, true)
	i.Step()
	told, logs = f.take()
	if len(told) != 1 || told[0] != "The \"Elite Dangerous\" controller profile is now in DSX. Start DSX again to use it." ||
		!strings.HasPrefix(logs, "DSX profile \"Elite Dangerous\": profile written to ") {
		t.Fatalf("written: told %q, log %q", told, logs)
	}
	if f.read(profilePath(f.dir)) != string(assets.DSXProfile) {
		t.Fatal("profile content")
	}
	wantState(t, i, StateDone, "")
	i.Step()
	wantState(t, i, StateDone, "")

	// once DSX runs again, the profile is simply there
	f.set(true, true)
	wantState(t, i, StatePresent, "")
	if err := i.Request(false); !errors.Is(err, ErrNothing) {
		t.Fatalf("install over a profile: %v", err)
	}
}

// TestInstallerFirstCheckLeaves: the first add never touches a profile
// DSX has, runs once, and leaves a job the player asked for alone.
func TestInstallerFirstCheckLeaves(t *testing.T) {
	f := newFakeDSX(t)
	f.write(profilePath(f.dir), "mine")
	i := f.installer()
	if !i.FirstCheck() || i.Job().State != "" {
		t.Fatal("the first check wants to add over the player's profile")
	}
	if err := os.Remove(profilePath(f.dir)); err != nil {
		t.Fatal(err)
	}
	if !i.FirstCheck() || i.Job().State != "" {
		t.Fatal("the first check ran twice")
	}
	if _, logs := f.take(); logs != "" {
		t.Fatalf("log %q", logs)
	}

	j := f.installer()
	if err := j.Request(true); err != nil {
		t.Fatal(err)
	}
	if !j.FirstCheck() || j.Job() != (Job{State: JobWaiting, Reset: true, Folder: f.dir}) {
		t.Fatalf("the first check replaced a reset: %+v", j.Job())
	}
}

// TestInstallerStates: missing, present, and a game profile for Elite that
// names another profile or none.
func TestInstallerStates(t *testing.T) {
	f := newFakeDSX(t)
	i := f.installer()
	wantState(t, i, StateMissing, "")
	f.write(profilePath(f.dir), "mine")
	wantState(t, i, StateNotForElite, "")
	f.write(gameProfilesPath(f.dir), string(utf8BOM)+`{"359320": {"ProfileName": "My Elite"}}`)
	wantState(t, i, StateNotForElite, "My Elite")
	f.write(gameProfilesPath(f.dir), `{"359320": {"ProfileName": "Elite Dangerous"}}`)
	wantState(t, i, StatePresent, "")
	if l := i.Look(); l.Folder != f.dir || !l.Has || l.ForElite != ProfileName || l.Running {
		t.Fatalf("look %+v", l)
	}
}

// TestInstallerReset: a reset waits while DSX runs, saying so once; it
// keeps copies of the player's profile and of DSX's game profiles, keeps
// the BOM and every number as written, and leaves no temporary file.
func TestInstallerReset(t *testing.T) {
	f := newFakeDSX(t)
	games := string(utf8BOM) + "{\r\n  \"359320\": {\r\n    \"ProfileName\": \"My Elite\",\r\n    \"ExePath\": \"x\",\r\n" +
		"    \"LastPlayed\": 638612345678901234,\r\n    \"Ratio\": 0.1\r\n  },\r\n  \"42\": {\r\n    \"ProfileName\": \"Other game\"\r\n  }\r\n}"
	f.write(gameProfilesPath(f.dir), games)
	f.write(profilePath(f.dir), "mine")
	i := f.installer()
	f.set(true, true)
	if err := i.Request(true); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		i.Step()
	}
	if told, logs := f.take(); len(told) != 0 || logs != "DSX profile: waiting for DSX to be closed\n" {
		t.Fatalf("waiting: told %q, log %q", told, logs)
	}
	wantState(t, i, StateWaiting, "")

	f.set(false, true)
	i.Step()
	told, logs := f.take()
	if len(told) != 1 || !strings.Contains(logs, "old profile saved as ") || !strings.Contains(logs, "game profiles saved as ") ||
		!strings.Contains(logs, "DSX will use it when Elite Dangerous starts") {
		t.Fatalf("reset: told %q, log %q", told, logs)
	}
	if f.read(profilePath(f.dir)) != string(assets.DSXProfile) {
		t.Fatal("not reset")
	}
	if b := f.read(filepath.Join(f.backups, "Elite Dangerous-20261001-123045.dsx")); b != "mine" {
		t.Fatalf("profile copy %q", b)
	}
	if b := f.read(filepath.Join(f.backups, "GameProfilesUpdates-20261001-123045.json")); b != games {
		t.Fatalf("game profiles copy %q", b)
	}
	after := f.read(gameProfilesPath(f.dir))
	if !strings.HasPrefix(after, string(utf8BOM)) || !strings.Contains(after, "\r\n") || strings.Contains(strings.ReplaceAll(after, "\r\n", ""), "\n") {
		t.Fatalf("the BOM or the line ends are gone: %q", after)
	}
	for _, keep := range []string{"638612345678901234", "0.1", `"ExePath": "x"`, `"Other game"`, `"ProfileName": "Elite Dangerous"`} {
		if !strings.Contains(after, keep) {
			t.Errorf("game profiles lost %s: %q", keep, after)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(gameProfilesPath(f.dir)), "*.tmp")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(profilePath(f.dir)), "*.tmp")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
	wantState(t, i, StateDone, "")

	// a second reset in the same second keeps both copies
	f.write(profilePath(f.dir), "mine again")
	if err := i.Request(true); err != nil {
		t.Fatal(err)
	}
	i.Step()
	if b := f.read(filepath.Join(f.backups, "Elite Dangerous-20261001-123045-2.dsx")); b != "mine again" {
		t.Fatalf("second copy %q", b)
	}
	if b := f.read(filepath.Join(f.backups, "Elite Dangerous-20261001-123045.dsx")); b != "mine" {
		t.Fatalf("first copy %q", b)
	}
}

// TestInstallerNewGameProfiles: without DSX's game profiles, the file is
// made for Elite, without a BOM, and nothing is copied.
func TestInstallerNewGameProfiles(t *testing.T) {
	f := newFakeDSX(t)
	i := f.installer()
	if err := i.Request(false); err != nil {
		t.Fatal(err)
	}
	i.Step()
	raw := f.read(gameProfilesPath(f.dir))
	var games map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &games); err != nil || games[elite.SteamAppID]["ExePath"] != f.exe ||
		games[elite.SteamAppID]["ProfileName"] != ProfileName {
		t.Fatalf("game profiles %q %v", raw, err)
	}
	if copies, _ := filepath.Glob(filepath.Join(f.backups, "*")); len(copies) != 0 {
		t.Fatalf("copies of nothing: %v", copies)
	}
}

// TestInstallerCancel: a waiting write ends, and Step writes nothing.
func TestInstallerCancel(t *testing.T) {
	f := newFakeDSX(t)
	i := f.installer()
	if i.Cancel() {
		t.Fatal("cancelled nothing")
	}
	if err := i.Request(false); err != nil {
		t.Fatal(err)
	}
	if !i.Waiting() || !i.Cancel() || i.Waiting() {
		t.Fatal("not cancelled")
	}
	i.Step()
	if exists(profilePath(f.dir)) {
		t.Fatal("written after a cancel")
	}
	wantState(t, i, StateMissing, "")
}

// TestInstallerFails: a write that fails, or a folder gone by then, is
// told and logged as before, and the state keeps why, without folders.
func TestInstallerFails(t *testing.T) {
	f := newFakeDSX(t)
	i := f.installer()
	if err := i.Request(false); err != nil {
		t.Fatal(err)
	}
	f.set(false, false)
	if err := os.RemoveAll(filepath.Join(f.dir, "DSX_Savefile")); err != nil {
		t.Fatal(err)
	}
	i.Step()
	if told, logs := f.take(); len(told) != 1 || told[0] != "DSX's folder was not found, so the profile could not be written." || logs != "" {
		t.Fatalf("no folder: told %q, log %q", told, logs)
	}
	wantState(t, i, StateFailed, "DSX's folder was not found")

	// the profiles folder is a file: the profile cannot be written
	f.set(false, true)
	profiles := filepath.Dir(profilePath(f.dir))
	if err := os.MkdirAll(filepath.Dir(gameProfilesPath(f.dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	f.write(profiles, "in the way")
	if err := i.Request(false); err != nil {
		t.Fatal(err)
	}
	i.Step()
	told, logs := f.take()
	if len(told) != 1 || !strings.HasPrefix(told[0], "The DSX profile could not be written:\n") ||
		!strings.HasPrefix(logs, "DSX profile not written: ") {
		t.Fatalf("failed: told %q, log %q", told, logs)
	}
	s := i.State()
	if s.State != StateFailed || s.Msg == "" || strings.Contains(s.Msg, f.dir) {
		t.Fatalf("failed state %+v", s)
	}
	// a failure stays until the next request
	i.Step()
	f.set(true, true)
	if s := i.State(); s.State != StateFailed {
		t.Fatalf("failure gone: %+v", s)
	}
}

// TestInstallerFolderKept: a DSX found only while it runs (outside the
// Steam libraries) is written once it is closed, into the folder the
// request was made for; the card keeps that folder while DSX is closed.
func TestInstallerFolderKept(t *testing.T) {
	f := newFakeDSX(t)
	i := f.installer()
	i.o.Folder = func(running bool) string {
		if running {
			return f.dir
		}
		return ""
	}
	f.set(true, true)
	if err := i.Request(false); err != nil {
		t.Fatal(err)
	}
	f.set(false, true)
	if l := i.Look(); l.Folder != f.dir {
		t.Fatalf("closed: look %+v", l)
	}
	i.Step()
	if !exists(profilePath(f.dir)) || i.Job().State != JobDone {
		t.Fatalf("not written: %+v", i.Job())
	}

	// the first add too
	g := newFakeDSX(t)
	k := g.installer()
	k.o.Folder = func(running bool) string {
		if running {
			return g.dir
		}
		return ""
	}
	g.set(true, true)
	if !k.FirstCheck() || k.Job().Folder != g.dir {
		t.Fatalf("first add: %+v", k.Job())
	}
	g.set(false, true)
	k.Step()
	if !exists(profilePath(g.dir)) {
		t.Fatal("first add not written")
	}
	// a folder that no longer holds DSX's settings is forgotten
	if err := os.RemoveAll(filepath.Join(g.dir, "DSX_Savefile")); err != nil {
		t.Fatal(err)
	}
	if l := k.Look(); l.Folder != "" {
		t.Fatalf("gone: look %+v", l)
	}
}

// TestInstallerPlayerFirst: once the player asked for the profile or
// cancelled it, the first add leaves it to them.
func TestInstallerPlayerFirst(t *testing.T) {
	f := newFakeDSX(t)
	i := f.installer()
	if err := i.Request(false); err != nil {
		t.Fatal(err)
	}
	if !i.Cancel() {
		t.Fatal("not cancelled")
	}
	if !i.FirstCheck() || i.Job().State != "" {
		t.Fatalf("the first add after a cancel: %+v", i.Job())
	}
	j := f.installer()
	if err := j.Request(true); err != nil {
		t.Fatal(err)
	}
	j.Cancel()
	if !j.FirstCheck() || j.Job().State != "" {
		t.Fatalf("the first add after a reset was cancelled: %+v", j.Job())
	}
	if exists(profilePath(f.dir)) {
		t.Fatal("written")
	}
}

// TestInstallerFailedShown: a failed add is shown while the profile is
// still missing; once it is there (made by hand), the files are shown. A
// failed reset is shown until the next request.
func TestInstallerFailedShown(t *testing.T) {
	l := Look{Folder: "D", Has: true, ForElite: ProfileName}
	if s := StateOf(l, Job{State: JobFailed, Err: "x"}); s.State != StatePresent {
		t.Errorf("add failed, profile there: %+v", s)
	}
	if s := StateOf(Look{Folder: "D"}, Job{State: JobFailed, Err: "x"}); s.State != StateFailed || s.Msg != "x" {
		t.Errorf("add failed, profile missing: %+v", s)
	}
	if s := StateOf(l, Job{State: JobFailed, Reset: true, Err: "x"}); s.State != StateFailed {
		t.Errorf("reset failed: %+v", s)
	}
	if s := StateOf(l, Job{State: JobWaiting, Writing: true}); s.State != StateWriting {
		t.Errorf("writing: %+v", s)
	}
}

// TestInstallerBusy: while it writes, a request is refused and a cancel
// does nothing.
func TestInstallerBusy(t *testing.T) {
	f := newFakeDSX(t)
	started, release := make(chan struct{}), make(chan struct{})
	i := f.installer()
	i.o.Elite = func() string {
		close(started)
		<-release
		return f.exe
	}
	if err := i.Request(false); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		i.Step()
		close(done)
	}()
	<-started
	if err := i.Request(true); !errors.Is(err, ErrBusy) {
		t.Errorf("request while writing: %v", err)
	}
	if i.Cancel() {
		t.Error("cancelled while writing")
	}
	if j := i.Job(); !j.Writing || j.State != JobWaiting {
		t.Errorf("while writing: %+v", j)
	}
	if s := StateOf(Look{}, i.Job()); s.State != StateWriting {
		t.Errorf("while writing: %+v", s)
	}
	close(release)
	<-done
	wantState(t, i, StateDone, "")
	if i.Job().Writing {
		t.Error("writing after the write")
	}

	// a bug while writing fails the job; the Installer goes on
	i.o.Elite = func() string { panic("a bug") }
	if err := i.Request(true); err != nil {
		t.Fatal(err)
	}
	i.Step()
	if j := i.Job(); j.State != JobFailed || !strings.Contains(j.Err, "a bug") || j.Writing {
		t.Fatalf("panic: %+v", j)
	}
	if err := i.Request(true); err != nil {
		t.Errorf("after a panic: %v", err)
	}
}

// TestGyroReader: the DSX setup finds DSX's folder and reads what the
// profile DSX uses for Elite does with the gyro, writing nothing.
func TestGyroReader(t *testing.T) {
	f := newFakeDSX(t)
	found := false
	g := newGyroReader(func() bool { return false }, func(bool) string {
		if found {
			return f.dir
		}
		return ""
	})
	if _, known := g.GyroToMouse(); known {
		t.Fatal("known without a folder")
	}
	found = true
	f.write(gameProfilesPath(f.dir), `{"359320": {"ProfileName": "My Elite"}}`)
	f.write(controllerProfilePath(f.dir, "My Elite"), `{"controller_motion": {"motion_mode": "MOTION_TO_MOUSE"}}`)
	g.Step()
	if mouse, known := g.GyroToMouse(); !mouse || !known {
		t.Fatalf("mouse %v known %v", mouse, known)
	}
	if exists(profilePath(f.dir)) {
		t.Fatal("the reader wrote a profile")
	}
}
