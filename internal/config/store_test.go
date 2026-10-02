package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// logged collects what the package logs while f runs.
func logged(t *testing.T, f func()) string {
	t.Helper()
	var buf bytes.Buffer
	var mu sync.Mutex
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	}))
	log.SetFlags(0)
	defer func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	}()
	f()
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// current is a settings file of this version with a change from the
// defaults, as Save writes it.
func current(t *testing.T, change func(*Config)) string {
	t.Helper()
	cfg := Default()
	if change != nil {
		change(&cfg)
	}
	b, err := encode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	s, _, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func kicked(s *Store) bool {
	select {
	case <-s.Kick():
		return true
	default:
		return false
	}
}

// TestOpenStore: OpenStore reads the file as LoadInfo does, writes only a
// missing or older one, logs nothing and publishes rev 1.
func TestOpenStore(t *testing.T) {
	cur := current(t, nil)
	for _, c := range []struct {
		name, file string // "": no file
		origin     Origin
		written    bool
		firstRun   bool
		problem    bool
		version    int
		backend    string
	}{
		{"created", "", Created, true, true, false, Version, ""},
		{"current", cur, Current, false, true, false, Version, ""},
		{"current, chosen", current(t, func(c *Config) { c.Backend = BackendDS4Windows }), Current, false, false, false, Version, BackendDS4Windows},
		{"migrated", `{"config_version": 4, "backend": ""}`, Migrated, true, false, false, Version, BackendAuto},
		{"newer", `{"config_version": 99, "backend": ""}`, Current, false, true, false, 99, ""},
		{"broken", `{"poll_ms": 30,}`, Broken, false, false, true, Version, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "edsense.json")
			var old time.Time
			if c.file != "" {
				old = writeOld(t, path, c.file)
			}
			var s *Store
			var info Info
			var err error
			if out := logged(t, func() { s, info, err = OpenStore(path) }); out != "" {
				t.Errorf("logged %q", out)
			}
			if info.Origin != c.origin || (err != nil) != c.problem {
				t.Errorf("origin %d, err %v", info.Origin, err)
			}
			snap := s.Snapshot()
			if snap.Rev != 1 || snap.FirstRun != c.firstRun || (snap.Problem != nil) != c.problem ||
				snap.Config.Version != c.version || snap.Config.Backend != c.backend || snap.From != "file" {
				t.Errorf("snapshot %+v", *snap)
			}
			if c.written {
				got, _, err := Read(path)
				if err != nil || !reflect.DeepEqual(got, snap.Config) {
					t.Errorf("written %+v, %v", got, err)
				}
			} else if c.file != "" {
				unchanged(t, path, c.file, old)
			}
			if kicked(s) {
				t.Error("the start kicked the loop")
			}
			if s.Path() != path {
				t.Errorf("path %s", s.Path())
			}
		})
	}
}

// TestStoreSet: Set writes, publishes a new rev, wakes the watchers and
// kicks the loop; Reload then finds its own write and publishes nothing.
func TestStoreSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	wake, stop := s.Watch()
	defer stop()
	var err error
	if out := logged(t, func() { err = s.Set(func(c *Config) { c.GyroAim = false }, "tray") }); err != nil || out != "" {
		t.Fatalf("Set: %v, logged %q", err, out)
	}
	snap := s.Snapshot()
	if snap.Rev != 2 || snap.Config.GyroAim || snap.From != "tray" {
		t.Errorf("snapshot %+v", *snap)
	}
	select {
	case <-wake:
	default:
		t.Error("the watcher was not woken")
	}
	if !kicked(s) {
		t.Error("the loop was not kicked")
	}
	if got, _, err := Read(path); err != nil || got.GyroAim {
		t.Errorf("file %+v, %v", got, err)
	}
	cfg, own, err := s.Reload()
	if err != nil || !own || cfg.GyroAim || s.Snapshot().Rev != 2 {
		t.Errorf("Reload: own %v, gyro_aim %v, rev %d, %v", own, cfg.GyroAim, s.Snapshot().Rev, err)
	}

	// a change to what the file already holds writes nothing
	st, _ := os.Stat(path)
	if err := s.Set(func(c *Config) { c.GyroAim = false }, "tray"); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(path); !st2.ModTime().Equal(st.ModTime()) || s.Snapshot().Rev != 2 || kicked(s) {
		t.Error("an unchanged Set wrote, published or kicked")
	}
	select {
	case <-wake:
		t.Error("an unchanged Set woke the watcher")
	default:
	}
}

// TestStoreForeign: an edit the Store did not write is not its own and
// publishes; the same bytes again publish nothing.
func TestStoreForeign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	if _, own, err := s.Reload(); err != nil || own || s.Snapshot().Rev != 1 {
		t.Errorf("unchanged file: own %v, rev %d, %v", own, s.Snapshot().Rev, err)
	}
	writeOld(t, path, current(t, func(c *Config) { c.PollMs = 40 }))
	cfg, own, err := s.Reload()
	if err != nil || own || cfg.PollMs != 40 {
		t.Fatalf("own %v, poll_ms %d, %v", own, cfg.PollMs, err)
	}
	snap := s.Snapshot()
	if snap.Rev != 2 || snap.Config.PollMs != 40 || snap.From != "file" {
		t.Errorf("snapshot %+v", *snap)
	}
	if _, _, err := s.Reload(); err != nil || s.Snapshot().Rev != 2 {
		t.Errorf("the same bytes published again: rev %d", s.Snapshot().Rev)
	}
	if kicked(s) {
		t.Error("Reload kicked the loop")
	}
}

// TestStoreOwnWrites: Reload counts the Store's last write as its own when
// it reads it first, and again while the file keeps the time it had then.
// An editor's save is not its own, even of bytes the Store wrote before or
// wrote last, and neither is a write that carried an editor's change.
func TestStoreOwnWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, `{"config_version": 4, "backend": ""}`)
	s := openStore(t, path)
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	set := func(change func(c *Config)) {
		t.Helper()
		if err := s.Set(change, "tray"); err != nil {
			t.Fatal(err)
		}
	}
	reload := func(what string, want bool) {
		t.Helper()
		if _, own, err := s.Reload(); err != nil || own != want {
			t.Errorf("%s: own %v, %v", what, own, err)
		}
	}
	reload("the start's write", true)
	set(func(c *Config) { c.PollMs = 30 })
	set(func(c *Config) { c.PollMs = 31 })
	reload("the last write", true)
	reload("the last write read again", true)

	writeOld(t, path, string(migrated))
	reload("the start's write put back by an editor", false)
	if snap := s.Snapshot(); snap.Config.PollMs != Default().PollMs {
		t.Errorf("not published: %+v", *snap)
	}

	set(func(c *Config) { c.PollMs = 32 })
	reload("the last write", true)
	last, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeOld(t, path, string(last))
	reload("the last write saved again by an editor", false)
	reload("that save read again", false)

	writeOld(t, path, current(t, func(c *Config) { c.PollMs, c.GyroAim = 33, false }))
	set(func(c *Config) { c.PollMs = 34 })
	reload("a write that carries an editor's change", false)
	if got, _, err := Read(path); err != nil || got.PollMs != 34 || got.GyroAim {
		t.Errorf("written %+v, %v", got, err)
	}
}

// TestStorePatch: a patch from the window is written and logged with what
// changed; problems and empty patches write nothing.
func TestStorePatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	var res PatchResult
	var err error
	out := logged(t, func() { res, err = s.Patch([]byte(`{"poll_ms": 40, "colors": {"hull_full": [1, 2, 3]}}`), "window") })
	if err != nil || !res.Applied || res.Rev != 2 || !reflect.DeepEqual(res.Changed, []string{"poll_ms", "colors.hull_full"}) || res.Problems != nil {
		t.Fatalf("%+v, %v", res, err)
	}
	if out != "Settings changed in the window: poll_ms, colors.hull_full\n" {
		t.Errorf("logged %q", out)
	}
	if snap := s.Snapshot(); snap.Config.PollMs != 40 || snap.Config.Colors["hull_full"] != [3]int{1, 2, 3} || snap.From != "window" {
		t.Errorf("snapshot %+v", *snap)
	}
	if !kicked(s) {
		t.Error("the loop was not kicked")
	}
	if _, own, err := s.Reload(); err != nil || !own {
		t.Errorf("own %v, %v", own, err)
	}

	raw, _ := os.ReadFile(path)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{`{"poll_ms": 400}`, `{}`, `{"poll_ms": 40}`, `[1]`} {
		out := logged(t, func() { res, err = s.Patch([]byte(p), "window") })
		if err != nil || res.Rev != 2 || res.Changed != nil || out != "" {
			t.Errorf("%s: %+v, %v, logged %q", p, res, err, out)
		}
		if refused := p == `{"poll_ms": 400}` || p == `[1]`; res.Applied == refused || refused != (len(res.Problems) > 0) {
			t.Errorf("%s: %+v", p, res)
		}
		unchanged(t, path, string(raw), old)
		if kicked(s) {
			t.Errorf("%s kicked the loop", p)
		}
	}
}

// TestStoreBroken: a file that does not parse is never written; Reload
// publishes its problem with the last good settings, and a fixed file
// clears it.
func TestStoreBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, func(c *Config) { c.PollMs = 40 }))
	s := openStore(t, path)
	broken := `{"poll_ms": 30,}`
	old := writeOld(t, path, broken)

	cfg, own, err := s.Reload()
	var fe *FileError
	if !errors.As(err, &fe) || own || cfg.PollMs != 0 || !strings.HasPrefix(err.Error(), "edsense.json: ") {
		t.Fatalf("Reload: %+v, own %v, %v", cfg, own, err)
	}
	snap := s.Snapshot()
	if snap.Rev != 2 || snap.Problem == nil || snap.Problem.Line != 1 || snap.Config.PollMs != 40 || snap.FirstRun {
		t.Errorf("snapshot %+v", *snap)
	}
	if _, _, err := s.Reload(); err == nil || s.Snapshot().Rev != 2 {
		t.Errorf("the same broken bytes published again: %v", err)
	}

	if err := s.Set(func(c *Config) { c.GyroAim = false }, "tray"); !errors.As(err, &fe) {
		t.Errorf("Set: %v", err)
	}
	res, err := s.Patch([]byte(`{"poll_ms": 50}`), "window")
	if err != nil || res.Applied || res.Broken == nil || len(res.Problems) != 1 || res.Problems[0].Code != CodeBroken || res.Rev != 2 {
		t.Errorf("Patch: %+v, %v", res, err)
	}
	unchanged(t, path, broken, old)
	if kicked(s) {
		t.Error("the loop was kicked")
	}

	writeOld(t, path, current(t, func(c *Config) { c.PollMs = 50 }))
	if cfg, _, err := s.Reload(); err != nil || cfg.PollMs != 50 {
		t.Fatalf("fixed: %d, %v", cfg.PollMs, err)
	}
	if snap := s.Snapshot(); snap.Rev != 3 || snap.Problem != nil || snap.Config.PollMs != 50 {
		t.Errorf("fixed: %+v", *snap)
	}
}

// TestStoreBrokenAtStart: a file that does not parse at start gives the
// defaults and no first run.
func TestStoreBrokenAtStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, "\xff\xfe{\x00}\x00")
	s, info, err := OpenStore(path)
	snap := s.Snapshot()
	if err == nil || info.Origin != Broken || snap.Problem == nil || snap.FirstRun || !reflect.DeepEqual(snap.Config, Default()) {
		t.Errorf("%+v, %v", *snap, err)
	}
	res, err := s.Patch([]byte(`{"poll_ms": 50}`), "window")
	if err != nil || res.Broken == nil || !strings.Contains(res.Problems[0].Msg, "UTF-16") {
		t.Errorf("%+v, %v", res, err)
	}
}

// TestStoreMissing: Set and Patch on a missing file start from the
// defaults; Reload of a missing file publishes nothing.
func TestStoreMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, func(c *Config) { c.PollMs = 40 }))
	s := openStore(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Reload(); err == nil || s.Snapshot().Rev != 1 || s.Snapshot().Config.PollMs != 40 {
		t.Errorf("Reload of a missing file: %v, %+v", err, *s.Snapshot())
	}
	if err := s.Set(func(c *Config) { c.GyroAim = false }, "tray"); err != nil {
		t.Fatal(err)
	}
	got, info, err := Read(path)
	want := Default()
	want.GyroAim = false
	if err != nil || info.Origin != Current || !reflect.DeepEqual(got, want) {
		t.Errorf("written %+v, %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Patch([]byte(`{"poll_ms": 50}`), "window"); err != nil || !res.Applied {
		t.Fatalf("%+v, %v", res, err)
	}
	if got, _, _ := Read(path); got.PollMs != 50 || !got.GyroAim {
		t.Errorf("patched from %+v", got)
	}
}

// TestReloadNeverWrites: a file from an older version, pasted while
// EDSense runs, is migrated in memory only; the next Set writes it.
func TestReloadNeverWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	v4 := `{"config_version": 4, "backend": "", "poll_ms": 40}`
	old := writeOld(t, path, v4)
	cfg, own, err := s.Reload()
	if err != nil || own || cfg.Version != Version || cfg.Backend != BackendAuto || cfg.PollMs != 40 {
		t.Fatalf("%+v, own %v, %v", cfg, own, err)
	}
	if snap := s.Snapshot(); snap.Rev != 2 || snap.Config.Backend != BackendAuto || snap.FirstRun {
		t.Errorf("snapshot %+v", *snap)
	}
	unchanged(t, path, v4, old)
	if _, _, err := s.Reload(); err != nil || s.Snapshot().Rev != 2 {
		t.Errorf("read again: rev %d, %v", s.Snapshot().Rev, err)
	}
	unchanged(t, path, v4, old)
	if err := s.Set(func(c *Config) { c.GyroAim = false }, "tray"); err != nil {
		t.Fatal(err)
	}
	if got, info, err := Read(path); err != nil || info.From != Version || got.Backend != BackendAuto || got.PollMs != 40 || got.GyroAim {
		t.Errorf("written %+v, %+v, %v", got, info, err)
	}
}

// TestStoreKeepsNewerVersion: Set and Patch keep a newer file's version.
func TestStoreKeepsNewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, `{"config_version": 99}`)
	s := openStore(t, path)
	if err := s.Set(func(c *Config) { c.GyroAim = false }, "tray"); err != nil {
		t.Fatal(err)
	}
	if _, info, _ := Read(path); info.From != 99 {
		t.Errorf("Set wrote version %d", info.From)
	}
	if res, err := s.Patch([]byte(`{"poll_ms": 40}`), "window"); err != nil || !res.Applied {
		t.Fatalf("%+v, %v", res, err)
	}
	if _, info, _ := Read(path); info.From != 99 || s.Snapshot().Config.Version != 99 {
		t.Errorf("Patch wrote version %d", info.From)
	}
}

// TestStoreWriteFails: a failed write publishes nothing, kicks nothing and
// forgets its hash.
func TestStoreWriteFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "edsense.json")
	s, info, err := OpenStore(path)
	if err == nil || info.Origin != Created {
		t.Fatalf("%+v, %v", info, err)
	}
	if err := s.Set(func(c *Config) { c.GyroAim = false }, "tray"); err == nil {
		t.Fatal("Set wrote into a missing folder")
	}
	if s.Snapshot().Rev != 1 || !s.Snapshot().Config.GyroAim || kicked(s) {
		t.Errorf("published or kicked: %+v", *s.Snapshot())
	}
	if s.own != (ownWrite{}) {
		t.Error("a failed write is remembered")
	}
}

// TestReloadWhileWriting: Reload never waits for a write under way; it
// asks for another read, which Recheck reports once.
func TestReloadWhileWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	writeOld(t, path, current(t, func(c *Config) { c.PollMs = 40 }))
	s.mu.Lock()
	done := make(chan error)
	go func() {
		_, _, err := s.Reload()
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrRetry) {
			t.Errorf("Reload: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Reload waited for the write")
	}
	s.mu.Unlock()
	if s.Snapshot().Rev != 1 {
		t.Error("published while a write ran")
	}
	if !s.Recheck() || s.Recheck() {
		t.Error("Recheck does not report once")
	}
	if cfg, _, err := s.Reload(); err != nil || cfg.PollMs != 40 || s.Snapshot().Rev != 2 {
		t.Errorf("read again: %d, %v", cfg.PollMs, err)
	}
}

// TestStoreSnapshotCopies: the Snapshot shares no map or list with what
// Reload hands the loop, nor with what Set was given.
func TestStoreSnapshotCopies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	var given *Config
	if err := s.Set(func(c *Config) { c.PollMs, given = 40, c }, "tray"); err != nil {
		t.Fatal(err)
	}
	set := s.Snapshot()
	want := set.Config.Clone()
	given.Colors["hull_full"] = [3]int{9, 9, 9}
	if !reflect.DeepEqual(set.Config, want) {
		t.Error("Set's settings share the Snapshot's")
	}
	writeOld(t, path, current(t, func(c *Config) { c.PollMs = 50 }))
	cfg, _, err := s.Reload()
	if err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	want = snap.Config.Clone()
	cfg.Colors["hull_full"] = [3]int{1, 1, 1}
	cfg.TriggerFX["hit"].Params[0] = 99
	cfg.GyroOffGuiFocus[0] = 99
	cfg.HapticsGain["hit"] = 2
	if !reflect.DeepEqual(snap.Config, want) || snap.Config.PollMs != 50 {
		t.Error("the loop's settings share the Snapshot's")
	}
}

// TestClone: every map and list in Config is copied, and the copy is equal
// to the original, nil lists included.
func TestClone(t *testing.T) {
	c := Default()
	c.FireGroups["1"] = FireGroup{Primary: "beam", Secondary: "auto"}
	c.HUDColors["shield"] = "#112233"
	c.TriggerFX["hit"] = Trigger{Mode: "OFF"}
	d := c.Clone()
	if !reflect.DeepEqual(c, d) {
		t.Fatal("the copy differs")
	}
	cv, dv := reflect.ValueOf(c), reflect.ValueOf(d)
	for i := range cv.NumField() {
		switch f := cv.Field(i); f.Kind() {
		case reflect.Map, reflect.Slice:
			if f.Len() > 0 && f.UnsafePointer() == dv.Field(i).UnsafePointer() {
				t.Errorf("%s is shared", cv.Type().Field(i).Name)
			}
		}
	}
	d.TriggerFX["ship_weapons_r"].Params[0] = 99
	if c.TriggerFX["ship_weapons_r"].Params[0] == 99 {
		t.Error("trigger params are shared")
	}
	if d.TriggerFX["hit"].Params != nil {
		t.Error("nil params became a list")
	}
}

// TestPatchReloadRace: an editor saves, then a patch and the loop's
// Reload run at once, again and again. However they interleave, the
// Snapshot is what the file holds once both are done and the loop has read
// again when Recheck asked it to. Run with -race.
func TestPatchReloadRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(io.Discard) // the patches' lines
	defer func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	}()
	wake, stop := s.Watch()
	defer stop()
	var last int64
	for i := range 100 {
		foreign := current(t, func(c *Config) { c.Brightness = i })
		if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var res PatchResult
		var perr, rerr error
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			res, perr = s.Patch(fmt.Appendf(nil, `{"poll_ms": %d}`, 30+i%70), "window")
		}()
		go func() {
			defer wg.Done()
			<-start
			if _, _, rerr = s.Reload(); errors.Is(rerr, ErrRetry) {
				rerr = nil
			}
		}()
		close(start)
		wg.Wait()
		if s.Recheck() { // the next tick
			_, _, rerr = s.Reload()
		}
		if perr != nil || rerr != nil || !res.Applied {
			t.Fatalf("round %d: %+v, %v, %v", i, res, perr, rerr)
		}
		got, _, err := Read(path)
		snap := s.Snapshot()
		if err != nil || snap.Problem != nil || !reflect.DeepEqual(got, snap.Config) {
			t.Fatalf("round %d: the Snapshot is not the file: poll_ms %d/%d, brightness %d/%d, %v",
				i, snap.Config.PollMs, got.PollMs, snap.Config.Brightness, got.Brightness, err)
		}
		if snap.Rev <= last {
			t.Fatalf("round %d: rev %d after %d", i, snap.Rev, last)
		}
		last = snap.Rev
		select {
		case <-wake:
		default:
			t.Fatalf("round %d: the watcher was not woken", i)
		}
	}
	// the last patch may have carried the editor's save, so it need not
	// be the Store's own; either way the Snapshot is already the file
	if _, _, err := s.Reload(); err != nil || s.Snapshot().Rev != last {
		t.Errorf("a last read: rev %d/%d, %v", s.Snapshot().Rev, last, err)
	}
}

// TestReloadUnreadable: a file that could not be read publishes nothing:
// the Snapshot keeps its settings and shows no problem, and the next read
// takes the file as it is then.
func TestReloadUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil { // a folder cannot be read as the file
		t.Fatal(err)
	}
	_, own, err := s.Reload()
	var unread *UnreadError
	if !errors.As(err, &unread) || own {
		t.Fatalf("Reload: own %v, %v", own, err)
	}
	if snap := s.Snapshot(); snap.Rev != 1 || snap.Problem != nil {
		t.Errorf("published: %+v", *snap)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeOld(t, path, current(t, func(c *Config) { c.PollMs = 40 }))
	if cfg, _, err := s.Reload(); err != nil || cfg.PollMs != 40 {
		t.Fatalf("read again: %d, %v", cfg.PollMs, err)
	}
	if snap := s.Snapshot(); snap.Rev != 2 || snap.Problem != nil || snap.Config.PollMs != 40 {
		t.Errorf("read again: %+v", *snap)
	}
}

// TestStoreCheck: Check reads the file as a change would and writes or
// publishes nothing: nil for a good or a missing file, the problem for one
// that does not parse or could not be read, before Reload has seen it.
func TestStoreCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	writeOld(t, path, current(t, nil))
	s := openStore(t, path)
	if fe, err := s.Check(); fe != nil || err != nil {
		t.Errorf("good: %v, %v", fe, err)
	}
	broken := `{"poll_ms": 30,}`
	old := writeOld(t, path, broken)
	if fe, err := s.Check(); fe == nil || fe.Line != 1 || err == nil {
		t.Errorf("broken: %+v, %v", fe, err)
	}
	unchanged(t, path, broken, old)
	if s.Snapshot().Rev != 1 || s.Snapshot().Problem != nil || kicked(s) {
		t.Error("Check published or kicked")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if fe, err := s.Check(); fe == nil || err == nil || !strings.Contains(fe.Msg, "could not be read") {
		t.Errorf("unreadable: %+v, %v", fe, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if fe, err := s.Check(); fe != nil || err != nil {
		t.Errorf("missing: %v, %v", fe, err)
	}
}

// TestPatchFolders: Patch sets a folder setting only to a folder that is
// there, and never looks for "" (Elite's standard folder) or a folder the
// patch does not change. A refused folder writes nothing, and its message
// holds no path.
func TestPatchFolders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edsense.json")
	gone := filepath.Join(dir, "gone")
	writeOld(t, path, current(t, func(c *Config) { c.BindingsDir = gone }))
	s := openStore(t, path)
	patch := func(fields map[string]any) []byte {
		b, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	journal := filepath.Join(dir, "Journal")
	if err := os.Mkdir(journal, 0o755); err != nil {
		t.Fatal(err)
	}
	var res PatchResult
	var err error
	out := logged(t, func() { res, err = s.Patch(patch(map[string]any{"journal_dir": journal}), "window") })
	if err != nil || !res.Applied || res.Rev != 2 || !reflect.DeepEqual(res.Changed, []string{"journal_dir"}) || res.Problems != nil {
		t.Fatalf("%+v, %v", res, err)
	}
	if out != "Settings changed in the window: journal_dir\n" {
		t.Errorf("logged %q", out)
	}
	if got, _, err := Read(path); err != nil || got.JournalDir != journal || got.BindingsDir != gone {
		t.Errorf("written %q %q, %v", got.JournalDir, got.BindingsDir, err)
	}
	kicked(s)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"journal_dir": filepath.Join(dir, "missing")}, "journal_dir missing"},
		{map[string]any{"journal_dir": file}, "journal_dir missing"},
		{map[string]any{"journal_dir": "", "bindings_dir": file, "poll_ms": 40}, "bindings_dir missing"},
	} {
		p := patch(c.fields)
		out := logged(t, func() { res, err = s.Patch(p, "window") })
		if err != nil || res.Applied || res.Rev != 2 || res.Changed != nil || out != "" || problemsText(res.Problems) != c.want {
			t.Errorf("%s: %+v, %v, logged %q", p, res, err, out)
		} else if msg := res.Problems[0].Msg; msg != "no such folder" {
			t.Errorf("%s: message %q", p, msg)
		}
		unchanged(t, path, string(raw), old)
		if kicked(s) {
			t.Errorf("%s kicked the loop", p)
		}
	}

	// a folder the patch leaves alone is not looked for, and "" is taken
	logged(t, func() { res, err = s.Patch(patch(map[string]any{"poll_ms": 40, "journal_dir": ""}), "window") })
	if err != nil || !res.Applied || !reflect.DeepEqual(res.Changed, []string{"journal_dir", "poll_ms"}) {
		t.Fatalf("%+v, %v", res, err)
	}
	if got, _, err := Read(path); err != nil || got.JournalDir != "" || got.BindingsDir != gone || got.PollMs != 40 {
		t.Errorf("written %q %q %d, %v", got.JournalDir, got.BindingsDir, got.PollMs, err)
	}
}
