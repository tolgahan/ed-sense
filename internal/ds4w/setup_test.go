package ds4w

import (
	"bytes"
	"errors"
	"log"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestOnce: each warning is first once, whoever asks.
func TestOnce(t *testing.T) {
	var o Once
	if !o.First(WarnTriggerLab) || o.First(WarnTriggerLab) {
		t.Fatal("TriggerLab not first exactly once")
	}
	if !o.First(WarnDSXOnPort) {
		t.Fatal("another warning is not first")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	firsts := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if o.First(WarnOldVersion) {
				mu.Lock()
				firsts++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firsts != 1 {
		t.Fatalf("first %d times at once", firsts)
	}
}

// TestSetupReport: each check publishes a new Report with the profile in
// use, where it came from, its checks and the warnings found; a Report
// once published does not change.
func TestSetupReport(t *testing.T) {
	f := &fakeEnv{dir: dataCopy(t), elite: "EliteDangerous64.exe", version: "4.9.1.0", dsx: true}
	s := newSetup(f.env(), nil)
	if s.Report() != nil {
		t.Fatal("a report before the first check")
	}
	s.check(true)
	first := s.Report()
	want := Report{Profile: "Elite Passthru", Source: "from Profiles.xml", Output: "ViiperDualSense", Gyro: GyroFree,
		Touchpad: "Passthru", Warnings: []Warning{WarnOldVersion, WarnDSXOnPort}}
	if first == nil || !reflect.DeepEqual(*first, want) {
		t.Fatalf("from Profiles.xml: %+v", first)
	}

	s.Game(true, true, false)
	s.check(false)
	want = Report{Profile: "Elite Mouse", Source: "from Auto Profiles.xml", Output: "ViiperDualSense", Gyro: GyroMouse,
		Touchpad: "Mouse", TriggerLab: true, Warnings: []Warning{WarnOldVersion, WarnDSXOnPort, WarnTriggerLab, WarnTouchpadMouse}}
	if r := s.Report(); !reflect.DeepEqual(*r, want) {
		t.Fatalf("from Auto Profiles.xml: %+v", r)
	}
	if first.Profile != "Elite Passthru" || len(first.Warnings) != 2 {
		t.Fatalf("the first report changed: %+v", first)
	}

	f.answer = map[string]string{PropProfile: "Elite Controls", PropOutput: "ViiperX360"}
	s.check(false)
	want = Report{Profile: "Elite Controls", Source: "DS4Windows says so", Output: "ViiperX360", Gyro: GyroOther,
		Touchpad: "Passthru", Warnings: []Warning{WarnOldVersion, WarnDSXOnPort, WarnNotDualSense}}
	if r := s.Report(); !reflect.DeepEqual(*r, want) {
		t.Fatalf("asked: %+v", r)
	}

	f.answer = map[string]string{PropProfile: "Gone"}
	s.check(false)
	if r := s.Report(); r.Profile != "Gone" || r.Problem == "" || r.Gyro != GyroUnknown || r.Output != "" {
		t.Fatalf("a profile without a file: %+v", r)
	}

	f.answer, f.dir = nil, ""
	s.check(false)
	if r := s.Report(); r.Profile != "" || r.Source != "DS4Windows' folder not found" || r.Gyro != GyroUnknown {
		t.Fatalf("no folder: %+v", r)
	}
}

// TestSetupWarnedShared: Setups that share a Once tell each warning once
// between them, and each still reports what it found.
func TestSetupWarnedShared(t *testing.T) {
	once := &Once{}
	var told []Warning
	tell := func(w Warning, _ string) { told = append(told, w) }
	f := &fakeEnv{dir: dataCopy(t), version: "4.9.1.0", dsx: true}
	env := f.env()
	env.Warned = once

	a := newSetup(env, tell)
	a.check(true)
	b := newSetup(env, tell)
	b.check(true)
	if want := []Warning{WarnOldVersion, WarnDSXOnPort}; !reflect.DeepEqual(told, want) {
		t.Fatalf("told %v, want %v", told, want)
	}
	if r := b.Report(); !reflect.DeepEqual(r.Warnings, []Warning{WarnOldVersion, WarnDSXOnPort}) {
		t.Fatalf("the second Setup reports %v", r.Warnings)
	}

	own := newSetup(f.env(), tell)
	own.check(true)
	if len(told) != 4 {
		t.Fatalf("a Setup with its own set told %v", told)
	}
}

// lockedWriter is a log writer the test reads while goroutines log.
type lockedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestSetupClose: Close does not wait for a check under way; that check
// then tells, logs and publishes nothing; nothing is asked for after
// Close; and the worker ends.
func TestSetupClose(t *testing.T) {
	var logged lockedWriter
	old := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(old)
	base := runtime.NumGoroutine()

	f := &fakeEnv{dir: dataCopy(t)}
	env := f.env()
	asked, release := make(chan struct{}), make(chan struct{})
	env.Query = func(slot int, prop string) (string, error) {
		if prop != PropProfile {
			return "ViiperX360", nil // not a DualSense: a warning
		}
		close(asked)
		<-release
		return "Elite Controls", nil
	}
	var mu sync.Mutex
	var told []Warning
	s := NewSetup(env, func(w Warning, _ string) {
		mu.Lock()
		told = append(told, w)
		mu.Unlock()
	})
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("the first check never asked DS4Windows")
	}
	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waited for the check under way")
	}
	s.Close()
	s.Game(true, true, false)
	s.Game(true, true, true)
	s.Step()
	if len(s.kick) != 0 {
		t.Fatal("a check was asked for after Close")
	}
	close(release)
	waitGoroutines(t, base)
	mu.Lock()
	defer mu.Unlock()
	if len(told) != 0 {
		t.Errorf("told %v after Close", told)
	}
	if s.Report() != nil {
		t.Errorf("published %+v after Close", s.Report())
	}
	if got := logged.String(); got != "" {
		t.Errorf("logged after Close: %q", got)
	}
}

// TestSetupCloseIdle: a Setup closed between checks ends its worker, and
// one never started closes too.
func TestSetupCloseIdle(t *testing.T) {
	base := runtime.NumGoroutine()
	s := NewSetup(Env{Query: func(int, string) (string, error) { return "", errors.New("not running") }}, nil)
	select {
	case <-s.Checked():
	case <-time.After(2 * time.Second):
		t.Fatal("the first check never ended")
	}
	s.Close()
	waitGoroutines(t, base)
	newSetup(Env{}, nil).Close()
}

// TestSetupCheckNow: "Check again" checks at once, though Elite does not
// run; after Close it asks for nothing.
func TestSetupCheckNow(t *testing.T) {
	var mu sync.Mutex
	profile := "Elite Passthru"
	f := &fakeEnv{dir: dataCopy(t)}
	env := f.env()
	env.Query = func(slot int, prop string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if prop == PropProfile {
			return profile, nil
		}
		return "", errors.New("no answer")
	}
	s := NewSetup(env, nil)
	defer s.Close()
	select {
	case <-s.Checked():
	case <-time.After(2 * time.Second):
		t.Fatal("the first check never ended")
	}
	if r := s.Report(); r == nil || r.Profile != "Elite Passthru" {
		t.Fatalf("first report %+v", r)
	}
	mu.Lock()
	profile = "Elite Mouse"
	mu.Unlock()
	s.Step() // Elite does not run: no check
	time.Sleep(50 * time.Millisecond)
	if r := s.Report(); r.Profile != "Elite Passthru" {
		t.Fatalf("checked while Elite does not run: %+v", r)
	}
	s.CheckNow()
	for end := time.Now().Add(2 * time.Second); s.Report().Profile != "Elite Mouse"; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("CheckNow did not check: %+v", s.Report())
		}
	}
	if r := s.Report(); r.Gyro != GyroMouse {
		t.Errorf("after CheckNow: %+v", r)
	}
	s.Close()
	s.CheckNow()
	if len(s.kick) != 0 {
		t.Error("a check was asked for after Close")
	}
}

// waitGoroutines waits up to 2 s for the goroutines to be back to want.
func waitGoroutines(t *testing.T, want int) {
	t.Helper()
	n := 0
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if n = runtime.NumGoroutine(); n <= want {
			return
		}
	}
	t.Fatalf("%d goroutines, want %d", n, want)
}
