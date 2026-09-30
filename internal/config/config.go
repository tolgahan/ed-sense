// Package config reads and writes the settings file (edsense.json).
//
// The file is created with every default on first run so players can edit
// it. Missing keys keep their defaults; unknown effect, colour and trigger
// keys are dropped when the file is rewritten.
package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Version is bumped when a default changes in a way existing files should
// pick up; older files are rewritten with the new keys.
const Version = 4

// Controller apps: what EDSense drives the controller through.
const (
	BackendAuto       = "auto" // the one that runs; "" is the same until the first run asks
	BackendDSX        = "dsx"
	BackendDS4Windows = "ds4windows"
)

// Where native haptics go with DS4Windows.
const (
	DS4WHapticsAuto       = "auto"       // the controller's own audio device when wired, else the virtual pad's
	DS4WHapticsController = "controller" // the controller's own audio device
	DS4WHapticsVirtual    = "virtual"    // DS4Windows' virtual DualSense
)

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

// Gyro aim: who turns the controller's motion into mouse movement, and how
// slow movement is handled.
const (
	GyroByEDSense = "edsense"
	GyroByDSX     = "dsx"
	GyroLowDSX    = "dsx"   // very slow movement moves nothing, as with DSX
	GyroLowExact  = "exact" // every bit of rotation moves the mouse
)

type Config struct {
	Version int `json:"config_version"`
	// Backend: "auto", "dsx" or "ds4windows"; "" until the first run asks
	// (it works as "auto").
	Backend        string `json:"backend"`
	JournalDir     string `json:"journal_dir"`     // empty: Saved Games\Frontier Developments\Elite Dangerous
	BindingsDir    string `json:"bindings_dir"`    // empty: %LOCALAPPDATA%\Frontier Developments\Elite Dangerous\Options\Bindings
	DSXPort        int    `json:"dsx_port"`        // 0: read DSX's port file, else 6969
	DS4WindowsPort int    `json:"ds4windows_port"` // 0: DS4Windows' own setting, else 6969
	PollMs         int    `json:"poll_ms"`

	// Outputs set to false are left to the DSX or DS4Windows profile.
	Lightbar   bool `json:"control_lightbar"`
	Triggers   bool `json:"control_triggers"`
	PlayerLEDs bool `json:"control_player_leds"`
	MicLED     bool `json:"control_mic_led"`

	Haptics         bool    `json:"control_haptics"`
	HapticsStrength float64 `json:"haptics_strength"`
	HapticsMode     string  `json:"haptics_mode"`
	// DS4WindowsHaptics: where native haptics go with DS4Windows: auto,
	// controller or virtual.
	DS4WindowsHaptics string             `json:"ds4windows_haptics"`
	HapticsGain       map[string]float64 `json:"haptics_gain"` // per effect, 0 turns it off
	TurnFeel          string             `json:"turn_feel"`    // waves, push, off
	JumpFeel          string             `json:"jump_feel"`    // swell, calm, off
	// Fire groups by number (1-based). Values: auto, beam, pulse, burst,
	// multicannon, cannon, fragment, railgun, plasma, missile, mining, generic.
	FireGroups map[string]FireGroup `json:"fire_groups"`
	// SpinUpMs: how long multi-cannons spin up before firing, by hardpoint
	// size (small, medium, large, huge).
	SpinUpMs map[string]int `json:"spin_up_ms"`

	// GyroAim false: no gyro aim while Elite runs.
	GyroAim        bool `json:"gyro_aim"`
	GyroOffInMenus bool `json:"gyro_off_in_menus"`
	// GyroBy: "edsense" turns the motion into mouse movement itself, with
	// DSX's own motion to mouse off; "dsx" leaves it to DSX.
	GyroBy string `json:"gyro_by"`
	// Two plain numbers: a JSON array breaks the whole file when someone
	// writes one number. 1 is DSX's bundled profile.
	GyroSensitivityX  float64 `json:"gyro_sensitivity_x"`
	GyroSensitivityY  float64 `json:"gyro_sensitivity_y"`
	GyroRollMix       float64 `json:"gyro_roll_mix"`  // rolling the controller turns sideways by this share
	GyroLowSpeed      string  `json:"gyro_low_speed"` // dsx, exact
	GyroAutoCalibrate bool    `json:"gyro_auto_calibrate"`
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
// It writes only then and when the file is from an older version.
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
	// files from before the backend choice: DS4Windows users already
	// played through its DSX listener, so the app that runs is picked
	if cfg.Version < 4 && cfg.Backend == "" {
		cfg.Backend = BackendAuto
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

// Save writes the settings file: to a temporary file next to it, which then
// replaces it, so a crash or a full disk never leaves half a file.
func Save(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeReplacing(path, compact(b))
}

// A virus scanner or OneDrive may hold the file for a moment, so the
// rename is tried again before the file is written in place.
const (
	renameRetries = 5
	renameWait    = 50 * time.Millisecond
)

var (
	rename      = os.Rename // tests make it fail
	inPlaceOnce sync.Once
)

// writeReplacing writes path through path.tmp. When the temporary file
// cannot be written (a full disk), path is left as it was.
func writeReplacing(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := writeSynced(tmp, b); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	var err error
	for try := 0; try <= renameRetries; try++ {
		if try > 0 {
			time.Sleep(renameWait)
		}
		if err = rename(tmp, path); err == nil {
			return nil
		}
	}
	_ = os.Remove(tmp)
	inPlaceOnce.Do(func() {
		log.Printf("Settings: could not replace %s (%v), so it is written in place", filepath.Base(path), err)
	})
	return os.WriteFile(path, b, 0o644)
}

func writeSynced(path string, b []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
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
	switch c.GyroBy {
	case GyroByEDSense, GyroByDSX:
	default:
		c.GyroBy = d.GyroBy
	}
	switch c.GyroLowSpeed {
	case GyroLowDSX, GyroLowExact:
	default:
		c.GyroLowSpeed = d.GyroLowSpeed
	}
	switch c.Backend {
	case "", BackendAuto, BackendDSX, BackendDS4Windows:
	default:
		c.Backend = BackendAuto
	}
	switch c.DS4WindowsHaptics {
	case DS4WHapticsAuto, DS4WHapticsController, DS4WHapticsVirtual:
	default:
		c.DS4WindowsHaptics = DS4WHapticsAuto
	}
	if c.DS4WindowsPort < 0 || c.DS4WindowsPort > 65535 {
		c.DS4WindowsPort = 0
	}
	c.GyroSensitivityX = sensitivity(c.GyroSensitivityX)
	c.GyroSensitivityY = sensitivity(c.GyroSensitivityY)
	c.GyroRollMix = min(max(c.GyroRollMix, 0), 2)
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

// sensitivity: 1 for nothing or nonsense, else within 0.05 and 20.
func sensitivity(v float64) float64 {
	if v <= 0 {
		return 1
	}
	return min(max(v, 0.05), 20)
}

// BackendChoice is the backend setting as it works: "" is "auto".
func (c *Config) BackendChoice() string {
	if c.Backend == "" {
		return BackendAuto
	}
	return c.Backend
}

// RestartKeys names the keys read only at start that differ between old
// and new.
func RestartKeys(old, new *Config) []string {
	var keys []string
	add := func(changed bool, key string) {
		if changed {
			keys = append(keys, key)
		}
	}
	add(old.BackendChoice() != new.BackendChoice(), "backend")
	add(old.DS4WindowsPort != new.DS4WindowsPort, "ds4windows_port")
	add(old.DSXPort != new.DSXPort, "dsx_port")
	add(old.JournalDir != new.JournalDir, "journal_dir")
	add(old.BindingsDir != new.BindingsDir, "bindings_dir")
	add(old.PollMs != new.PollMs, "poll_ms")
	return keys
}
