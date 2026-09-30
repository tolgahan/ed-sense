package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCreatesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.GyroAim || cfg.Brightness != 200 || cfg.Version != Version || cfg.TurnFeel != TurnWaves || cfg.JumpFeel != JumpSwell {
		t.Fatalf("defaults: %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("settings file not created")
	}
}

func TestLoadMergesAndRewritesOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	old := `{"lightbar_brightness": 90, "turn_feel": "hum", "jump_feel": "calm", "gyro_by": "both", "gyro_sensitivity_x": 0, "gyro_sensitivity_y": 50, "gyro_roll_mix": -1, "gyro_low_speed": "exact", "haptics_gain": {"boost": 0.5, "danger": 1}, "rumble": {"old_effect": {"left": 1, "right": 1, "ms": 10}}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Brightness != 90 || cfg.Gain("boost") != 0.5 || cfg.Gain("maneuver") != 1 || !cfg.Haptics || cfg.TurnFeel != TurnWaves || cfg.JumpFeel != JumpCalm ||
		cfg.GyroBy != Default().GyroBy || cfg.GyroSensitivityX != 1 || cfg.GyroSensitivityY != 20 || cfg.GyroRollMix != 0 || cfg.GyroLowSpeed != GyroLowExact || !cfg.GyroAutoCalibrate {
		t.Fatalf("merge: %+v", cfg)
	}
	if _, ok := cfg.HapticsGain["danger"]; ok {
		t.Fatal("unknown gain kept")
	}
	if _, ok := cfg.Rumble["old_effect"]; ok {
		t.Fatal("unknown rumble effect kept")
	}
	raw, _ := os.ReadFile(path)
	var back Config
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("rewritten file is not valid JSON: %v", err)
	}
	if back.Brightness != 90 || back.Version != Version {
		t.Fatalf("rewritten: brightness %d, version %d", back.Brightness, back.Version)
	}
	for _, want := range []string{`"boost": {"left": 0.7, "right": 0.7, "ms": 900}`, `"ship_weapons_r": {"mode": "WEAPON", "params": [2, 5, 6]}`, `"turn_feel": "waves"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("not compacted, missing %s:\n%s", want, raw)
		}
	}
}

func TestLoadRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	if err := os.WriteFile(path, []byte(`{"lightbar_brightness": `), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err == nil {
		t.Fatal("broken file accepted")
	}
	if cfg.Brightness != 200 {
		t.Fatal("defaults not returned with the error")
	}
}

// TestMigrateBackend: a file from before the backend choice gets "auto",
// since DS4Windows users already played through its DSX listener; a new
// file leaves it to the first run (""), which works as "auto".
func TestMigrateBackend(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name, file, want string
	}{
		{"v3 without backend", `{"config_version": 3, "dsx_port": 6970}`, BackendAuto},
		{"no version", `{"lightbar_brightness": 90}`, BackendAuto},
		{"v3 with backend", `{"config_version": 3, "backend": "dsx"}`, BackendDSX},
		{"v4 not asked yet", `{"config_version": 4}`, ""},
		{"v4 ds4windows", `{"config_version": 4, "backend": "ds4windows"}`, BackendDS4Windows},
		{"unknown word", `{"config_version": 4, "backend": "xbox"}`, BackendAuto},
	} {
		path := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "-")+".json")
		if err := os.WriteFile(path, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Backend != c.want || cfg.BackendChoice() == "" {
			t.Errorf("%s: backend %q (works as %q), want %q", c.name, cfg.Backend, cfg.BackendChoice(), c.want)
		}
		if cfg.DS4WindowsHaptics != DS4WHapticsAuto || cfg.DS4WindowsPort != 0 {
			t.Errorf("%s: ds4windows keys %q %d", c.name, cfg.DS4WindowsHaptics, cfg.DS4WindowsPort)
		}
		back, err := Load(path)
		if err != nil || back.Backend != c.want || back.Version != Version {
			t.Errorf("%s: after the rewrite %q v%d (%v)", c.name, back.Backend, back.Version, err)
		}
	}
	path := filepath.Join(dir, "new.json")
	if cfg, err := Load(path); err != nil || cfg.Backend != "" || cfg.BackendChoice() != BackendAuto {
		t.Errorf("new file: backend %q (%v)", cfg.Backend, err)
	}
}

func TestDS4WindowsKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	for _, c := range []struct {
		file      string
		port      int
		haptics   string
		wantError bool
	}{
		{`{"config_version": 4, "ds4windows_port": 6970, "ds4windows_haptics": "controller"}`, 6970, DS4WHapticsController, false},
		{`{"config_version": 4, "ds4windows_port": 70000, "ds4windows_haptics": "speaker"}`, 0, DS4WHapticsAuto, false},
		{`{"config_version": 4, "ds4windows_port": -1, "ds4windows_haptics": "virtual"}`, 0, DS4WHapticsVirtual, false},
	} {
		if err := os.WriteFile(path, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil || cfg.DS4WindowsPort != c.port || cfg.DS4WindowsHaptics != c.haptics {
			t.Errorf("%s: port %d, haptics %q (%v)", c.file, cfg.DS4WindowsPort, cfg.DS4WindowsHaptics, err)
		}
	}
}

// TestLoadLeavesCurrentFile: a current file that parses is not written,
// even when values are set back to valid ones.
func TestLoadLeavesCurrentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	file := `{"config_version": 4, "lightbar_brightness": 900, "backend": "auto"}`
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Brightness != 200 {
		t.Fatalf("brightness %d (%v)", cfg.Brightness, err)
	}
	raw, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if string(raw) != file || !st.ModTime().Equal(old) {
		t.Fatalf("a current file was written: %s", raw)
	}
}

// TestSaveReplaces: Save writes a temporary file and renames it over the
// settings file; when the rename keeps failing it writes in place.
func TestSaveReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edsense.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Brightness = 42
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	if back, err := Load(path); err != nil || back.Brightness != 42 {
		t.Fatalf("saved: %d (%v)", back.Brightness, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("the temporary file was left behind")
	}

	tries := 0
	old := rename
	rename = func(from, to string) error {
		tries++
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: os.ErrPermission}
	}
	defer func() { rename = old }()
	cfg.Brightness = 43
	start := time.Now()
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	if tries != 1+renameRetries || time.Since(start) < renameRetries*renameWait {
		t.Errorf("rename tried %d times in %v", tries, time.Since(start))
	}
	if back, err := Load(path); err != nil || back.Brightness != 43 {
		t.Fatalf("written in place: %d (%v)", back.Brightness, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("the temporary file was left behind after writing in place")
	}
}

// TestSaveTmpFails: when the temporary file cannot be written (a full
// disk), Save fails and leaves the settings file as it was.
func TestSaveTmpFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edsense.json")
	cfg := Default()
	cfg.Brightness = 42
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	// a folder where the temporary file goes cannot be written as a file
	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Brightness = 12
	if err := Save(path, cfg); err == nil {
		t.Error("Save did not fail")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("the settings file was written in place:\n%s", after)
	}
}

func TestRestartKeys(t *testing.T) {
	a, b := Default(), Default()
	b.Backend = BackendAuto // "" works as "auto"
	b.Brightness = 10
	if keys := RestartKeys(&a, &b); len(keys) != 0 {
		t.Fatalf("no restart key changed: %v", keys)
	}
	b.Backend, b.DS4WindowsPort, b.PollMs, b.BindingsDir = BackendDS4Windows, 6970, 30, "x"
	if got := strings.Join(RestartKeys(&a, &b), ","); got != "backend,ds4windows_port,bindings_dir,poll_ms" {
		t.Fatalf("keys %s", got)
	}
}

func TestUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	if err := Update(path, func(c *Config) { c.GyroAim = false }); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.GyroAim {
		t.Fatalf("gyro aim still on (%v)", err)
	}
}
