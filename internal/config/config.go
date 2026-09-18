// Package config reads and writes the settings file (edsense.json).
//
// The file is created with every default on first run so players can edit
// it. Missing keys keep their defaults; unknown colour and trigger keys are
// dropped when the file is rewritten.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Version is bumped when a default changes in a way existing files should
// pick up; older files are rewritten with the new keys.
const Version = 1

// Trigger is an adaptive trigger effect by DSX mode name, e.g. WEAPON [2 5 6].
type Trigger struct {
	Mode   string `json:"mode"`
	Params []int  `json:"params"`
}

type Config struct {
	Version    int    `json:"config_version"`
	JournalDir string `json:"journal_dir"` // empty: Saved Games\Frontier Developments\Elite Dangerous
	DSXPort    int    `json:"dsx_port"`    // 0: read DSX's port file, else 6969
	PollMs     int    `json:"poll_ms"`

	// Outputs set to false are left to the DSX profile.
	Lightbar   bool `json:"control_lightbar"`
	Triggers   bool `json:"control_triggers"`
	PlayerLEDs bool `json:"control_player_leds"`
	MicLED     bool `json:"control_mic_led"`

	Brightness int                `json:"lightbar_brightness"` // 0-255
	Colors     map[string][3]int  `json:"colors"`
	TriggerFX  map[string]Trigger `json:"triggers"`
}

// Color is a lightbar colour by name (white if unknown).
func (c *Config) Color(name string) [3]int {
	if v, ok := c.Colors[name]; ok {
		return v
	}
	return [3]int{255, 255, 255}
}

// Load reads the settings file, creating it with the defaults if missing.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Default()
		return cfg, Save(path, cfg)
	}
	if err != nil {
		return Default(), err
	}
	cfg := Default()
	cfg.Version = 0
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Default(), fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	cfg.normalise()
	if cfg.Version != Version {
		cfg.Version = Version
		if err := Save(path, cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// Save writes the settings file.
func Save(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, compact(b), 0o644)
}

// normalise fills in keys the file lacks, drops unknown ones and clamps
// values to what the app can use. JSON replaces maps wholesale, so default
// keys are merged back in.
func (c *Config) normalise() {
	d := Default()
	c.Colors = mergeKnown(c.Colors, d.Colors)
	c.TriggerFX = mergeKnown(c.TriggerFX, d.TriggerFX)
	if c.PollMs < 20 {
		c.PollMs = 20
	}
	if c.Brightness < 0 || c.Brightness > 255 {
		c.Brightness = d.Brightness
	}
}

// mergeKnown keeps the file's values for the keys defaults has, and the
// defaults for the rest.
func mergeKnown[V any](file, defaults map[string]V) map[string]V {
	out := make(map[string]V, len(defaults))
	for k, v := range defaults {
		if fv, ok := file[k]; ok {
			v = fv
		}
		out[k] = v
	}
	return out
}
