package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCreatesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.GyroAim || cfg.Brightness != 200 || cfg.Version != Version {
		t.Fatalf("defaults: %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("settings file not created")
	}
}

func TestLoadMergesAndRewritesOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	old := `{"lightbar_brightness": 90, "haptics_gain": {"boost": 0.5, "danger": 1}, "rumble": {"old_effect": {"left": 1, "right": 1, "ms": 10}}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Brightness != 90 || cfg.Gain("boost") != 0.5 || cfg.Gain("maneuver") != 1 || !cfg.Haptics {
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
	for _, want := range []string{`"boost": {"left": 0.7, "right": 0.7, "ms": 900}`, `"ship_weapons_r": {"mode": "WEAPON", "params": [2, 5, 6]}`} {
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
