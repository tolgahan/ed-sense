// Package config reads and writes the settings file (edsense.json).
//
// The file is created with every default on first run so players can edit
// it. Missing keys keep their defaults; unknown effect, colour and trigger
// keys are dropped when the file is rewritten.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Version is bumped when a default changes in a way existing files should
// pick up; older files are rewritten with the new keys.
const Version = 2

// Trigger is an adaptive trigger effect by DSX mode name, e.g. WEAPON [2 5 6].
type Trigger struct {
	Mode   string `json:"mode"`
	Params []int  `json:"params"`
}

// FireGroup overrides the weapon feel of one fire group. "auto" reads it
// from the HUD's weapon lists.
type FireGroup struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
}

// Rumble is a one-shot effect for the rumble fallback: peak strength per
// side (0-1) and length.
type Rumble struct {
	Left  float64 `json:"left"`
	Right float64 `json:"right"`
	Ms    int     `json:"ms"`
}

// Haptics modes.
const (
	HapticsAuto   = "auto"
	HapticsNative = "native"
	HapticsRumble = "rumble"
)

// Turn feels: how turning the ship feels outside the throttle's blue zone.
const (
	TurnWaves = "waves" // soft swells about every 2 s while the ship turns
	TurnPush  = "push"  // a soft push when a turn starts, changes or ends
	TurnOff   = "off"
)

// Jump feels: how an FSD jump feels.
const (
	JumpSwell = "swell" // one soft swell into the hyperspace tunnel
	JumpCalm  = "calm"  // the swell, and a soft pulse every second while the drive charges
	JumpOff   = "off"
)

type Config struct {
	Version     int    `json:"config_version"`
	JournalDir  string `json:"journal_dir"`  // empty: Saved Games\Frontier Developments\Elite Dangerous
	BindingsDir string `json:"bindings_dir"` // empty: %LOCALAPPDATA%\Frontier Developments\Elite Dangerous\Options\Bindings
	DSXPort     int    `json:"dsx_port"`     // 0: read DSX's port file, else 6969
	PollMs      int    `json:"poll_ms"`

	// Outputs set to false are left to the DSX profile.
	Lightbar   bool `json:"control_lightbar"`
	Triggers   bool `json:"control_triggers"`
	PlayerLEDs bool `json:"control_player_leds"`
	MicLED     bool `json:"control_mic_led"`

	Haptics         bool               `json:"control_haptics"`
	HapticsStrength float64            `json:"haptics_strength"`
	HapticsMode     string             `json:"haptics_mode"`
	HapticsGain     map[string]float64 `json:"haptics_gain"` // per effect, 0 turns it off
	TurnFeel        string             `json:"turn_feel"`    // waves, push, off
	JumpFeel        string             `json:"jump_feel"`    // swell, calm, off
	// Fire groups by number (1-based). Values: auto, beam, pulse, burst,
	// multicannon, cannon, fragment, railgun, plasma, missile, mining, generic.
	FireGroups map[string]FireGroup `json:"fire_groups"`
	// SpinUpMs: how long multi-cannons spin up before firing, by hardpoint
	// size (small, medium, large, huge).
	SpinUpMs map[string]int `json:"spin_up_ms"`

	// GyroAim false switches DSX's motion output off while Elite runs.
	GyroAim        bool `json:"gyro_aim"`
	GyroOffInMenus bool `json:"gyro_off_in_menus"`
	// Status.json GuiFocus panels with the gyro off: 1-4 side/top/bottom
	// panels, 5 station services, 6 galaxy map, 7 system map, 8 orrery,
	// 9 FSS, 10 surface scanner, 11 codex.
	GyroOffGuiFocus []int `json:"gyro_off_gui_focus"`

	HUDReader bool              `json:"hud_reader"`
	HUDDebug  bool              `json:"hud_debug"`  // save HUD captures to hud_debug/
	HUDColors map[string]string `json:"hud_colors"` // shield, heat, hull, flame, flash: "#rrggbb" ("flash": "off")

	Brightness int                `json:"lightbar_brightness"` // 0-255
	Colors     map[string][3]int  `json:"colors"`
	TriggerFX  map[string]Trigger `json:"triggers"`
	Rumble     map[string]Rumble  `json:"rumble"`
}

// Color is a lightbar colour by name (white if unknown).
func (c *Config) Color(name string) [3]int {
	if v, ok := c.Colors[name]; ok {
		return v
	}
	return [3]int{255, 255, 255}
}

// Gain is an effect's strength (1 if not set).
func (c *Config) Gain(effect string) float64 {
	if v, ok := c.HapticsGain[effect]; ok {
		return v
	}
	return 1
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

// Update changes settings in the file. A running app picks the change up
// like any other edit.
func Update(path string, change func(*Config)) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	change(&cfg)
	return Save(path, cfg)
}

// normalise fills in keys the file lacks, drops unknown ones and clamps
// values to what the app can use. JSON replaces maps wholesale, so default
// keys are merged back in.
func (c *Config) normalise() {
	d := Default()
	c.Colors = mergeKnown(c.Colors, d.Colors)
	c.TriggerFX = mergeKnown(c.TriggerFX, d.TriggerFX)
	c.HapticsGain = mergeKnown(c.HapticsGain, d.HapticsGain)
	c.Rumble = mergeKnown(c.Rumble, d.Rumble)
	c.SpinUpMs = mergeKnown(c.SpinUpMs, d.SpinUpMs)
	for k, ms := range c.SpinUpMs {
		c.SpinUpMs[k] = min(max(ms, 0), 5000)
	}
	if c.FireGroups == nil {
		c.FireGroups = map[string]FireGroup{}
	}
	if c.HUDColors == nil {
		c.HUDColors = map[string]string{}
	}
	switch c.HapticsMode {
	case HapticsAuto, HapticsNative, HapticsRumble:
	default:
		c.HapticsMode = HapticsAuto
	}
	switch c.TurnFeel {
	case TurnWaves, TurnPush, TurnOff:
	default:
		c.TurnFeel = d.TurnFeel
	}
	switch c.JumpFeel {
	case JumpSwell, JumpCalm, JumpOff:
	default:
		c.JumpFeel = d.JumpFeel
	}
	if c.HapticsStrength < 0 || c.HapticsStrength > 3 {
		c.HapticsStrength = 1
	}
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
