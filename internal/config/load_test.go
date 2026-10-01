package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeOld writes a settings file with an mtime an hour ago, so a write
// shows in the mtime too.
func writeOld(t *testing.T, path, content string) time.Time {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return old
}

// unchanged fails when the file is not content with the mtime old.
func unchanged(t *testing.T, path, content string, old time.Time) {
	t.Helper()
	raw, _ := os.ReadFile(path)
	st, err := os.Stat(path)
	if err != nil || string(raw) != content || !st.ModTime().Equal(old) {
		t.Errorf("the file was written: %s (%v)", raw, err)
	}
}

// TestOrigin: what LoadInfo and Read say they found, and that Read never
// writes while LoadInfo writes only a missing or older file.
func TestOrigin(t *testing.T) {
	bom := "\xef\xbb\xbf"
	cur := fmt.Sprintf(`{"config_version": %d, "poll_ms": 30}`, Version)
	for _, c := range []struct {
		name, file string // file "": no file
		origin     Origin
		from       int
		written    bool // by LoadInfo
		poll       int
	}{
		{"created", "", Created, 0, true, 25},
		{"current", cur, Current, Version, false, 30},
		{"migrated", `{"config_version": 4, "poll_ms": 30}`, Migrated, 4, true, 30},
		{"no version", `{"poll_ms": 30}`, Migrated, 0, true, 30},
		{"broken by syntax", `{"poll_ms": 30,}`, Broken, 0, false, 25},
		{"broken by type", `{"poll_ms": "fast"}`, Broken, 0, false, 25},
		{"bom", bom + cur, Current, Version, false, 30},
		{"utf-16 le", "\xff\xfe{\x00}\x00", Broken, 0, false, 25},
		{"utf-16 be", "\xfe\xff\x00{\x00}", Broken, 0, false, 25},
		{"empty", "", Broken, 0, false, 25},
	} {
		for _, write := range []bool{false, true} {
			dir := t.TempDir()
			path := filepath.Join(dir, "edsense.json")
			var old time.Time
			if c.file != "" || c.name == "empty" {
				old = writeOld(t, path, c.file)
			}
			var cfg Config
			var info Info
			var err error
			if write {
				cfg, info, err = LoadInfo(path)
			} else {
				cfg, info, err = Read(path)
			}
			label := fmt.Sprintf("%s (write %v)", c.name, write)
			if info.Origin != c.origin || info.From != c.from || cfg.PollMs != c.poll {
				t.Errorf("%s: origin %d from %d poll %d", label, info.Origin, info.From, cfg.PollMs)
			}
			if (info.Origin == Broken) != (err != nil) || (info.Origin == Broken) != (info.Problem != nil) {
				t.Errorf("%s: error %v, problem %+v", label, err, info.Problem)
			}
			if c.origin == Broken && cfg.Version != Version {
				t.Errorf("%s: broken gives version %d", label, cfg.Version)
			}
			switch {
			case write && c.written:
				back, binfo, err := Read(path)
				if err != nil || binfo.Origin != Current || back.PollMs != c.poll || back.Version != Version {
					t.Errorf("%s: after the write %+v %d (%v)", label, binfo, back.PollMs, err)
				}
			case c.file == "" && c.name != "empty":
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("%s: Read created the file", label)
				}
			default:
				unchanged(t, path, c.file, old)
			}
		}
	}
}

// TestFileError: where a broken file is broken, counted on the bytes after
// a BOM, the column in characters.
func TestFileError(t *testing.T) {
	for _, c := range []struct {
		name, file string
		line, col  int
		key        string
		syntax     bool
	}{
		{"trailing comma", "{\n  \"a\": 1,\n  \"c\": \"x\",\n}", 4, 1, "", true},
		{"first line", `{x}`, 1, 2, "", true},
		{"bom", "\xef\xbb\xbf{x}", 1, 2, "", true},
		{"bom on line 2", "\xef\xbb\xbf{\n x}", 2, 2, "", true},
		{"crlf", "{\r\n  \"poll_ms\": 30,\r\n}", 3, 1, "", true},
		{"wide characters", "{\"journal_dir\": \"C:\\\\\u00fcber\u00e7\", x}", 1, 30, "", true}, // 32 in bytes
		{"end of input", "{\n  \"poll_ms\": ", 2, 13, "", true},
		{"empty", "", 1, 1, "", true},
		{"type", "{\n  \"lightbar_brightness\": \"high\"\n}", 2, 31, "lightbar_brightness", false},
		{"deep type", `{"triggers": {"hit": {"params": [1, "x"]}}}`, 1, 39, "triggers.hit.params.1", false},
	} {
		path := filepath.Join(t.TempDir(), "edsense.json")
		writeOld(t, path, c.file)
		_, info, err := Read(path)
		p := info.Problem
		if p == nil || p.Line != c.line || p.Col != c.col || p.Key != c.key || p.Msg == "" {
			t.Errorf("%s: %#v", c.name, p)
			continue
		}
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		var fe *FileError
		if errors.As(err, &se) != c.syntax || errors.As(err, &te) == c.syntax || !errors.As(err, &fe) || fe != p {
			t.Errorf("%s: %v does not reach its JSON error", c.name, err)
		}
		if !strings.HasPrefix(err.Error(), "edsense.json: ") {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// TestFileErrorUnreadable: UTF-16 and a file that cannot be read have no
// line, and the message names no full path.
func TestFileErrorUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edsense.json")
	writeOld(t, path, "\xff\xfe{\x00}\x00")
	_, info, err := Read(path)
	if p := info.Problem; err == nil || p == nil || p.Line != 0 || p.Msg != "edsense.json is saved as UTF-16; save it as UTF-8" {
		t.Errorf("utf-16: %+v (%v)", p, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil { // a folder cannot be read as a file
		t.Fatal(err)
	}
	cfg, info, err := LoadInfo(path)
	p := info.Problem
	if err == nil || info.Origin != Broken || p == nil || p.Line != 0 || cfg.PollMs != 25 {
		t.Fatalf("unreadable: %+v %+v (%v)", info, p, err)
	}
	if !strings.HasPrefix(p.Msg, "edsense.json could not be read: ") || strings.Contains(p.Msg, dir) {
		t.Errorf("unreadable: %q", p.Msg)
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		t.Error("an unreadable file was written over")
	}
}

// TestNewerFile: a file from a newer EDSense is read and not rewritten,
// and a change keeps its config_version.
func TestNewerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	file := `{"config_version": 99, "poll_ms": 30, "backend": "", "from_the_future": true}`
	old := writeOld(t, path, file)
	cfg, info, err := LoadInfo(path)
	if err != nil || info.Origin != Current || info.From != 99 || cfg.Version != 99 || cfg.PollMs != 30 || cfg.Backend != "" {
		t.Fatalf("newer: %+v version %d poll %d backend %q (%v)", info, cfg.Version, cfg.PollMs, cfg.Backend, err)
	}
	unchanged(t, path, file, old)
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	unchanged(t, path, file, old)
	if err := Update(path, func(c *Config) { c.PollMs = 40 }); err != nil {
		t.Fatal(err)
	}
	back, info, err := Read(path)
	if err != nil || back.Version != 99 || back.PollMs != 40 || info.From != 99 {
		t.Errorf("after a change: version %d poll %d (%v)", back.Version, back.PollMs, err)
	}
}
