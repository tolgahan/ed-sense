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
	if !cfg.Triggers || cfg.Brightness != 200 || cfg.Version != Version {
		t.Fatalf("defaults: %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("settings file not created")
	}
}

func TestLoadMergesAndRewritesOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	old := `{"lightbar_brightness": 90, "colors": {"kill": [0, 0, 255], "danger": [1, 2, 3]}, "triggers": {"old_trigger": {"mode": "FEEDBACK", "params": [1, 1]}}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Brightness != 90 || cfg.Color("kill") != [3]int{0, 0, 255} || cfg.Color("hit") != [3]int{255, 30, 30} || !cfg.Triggers {
		t.Fatalf("merge: %+v", cfg)
	}
	if _, ok := cfg.Colors["danger"]; ok {
		t.Fatal("unknown colour kept")
	}
	if _, ok := cfg.TriggerFX["old_trigger"]; ok {
		t.Fatal("unknown trigger kept")
	}
	raw, _ := os.ReadFile(path)
	var back Config
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("rewritten file is not valid JSON: %v", err)
	}
	if back.Brightness != 90 || back.Version != Version {
		t.Fatalf("rewritten: brightness %d, version %d", back.Brightness, back.Version)
	}
	for _, want := range []string{`"kill": [0, 0, 255]`, `"ship_weapons_r": {"mode": "WEAPON", "params": [2, 5, 6]}`} {
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
