// Package bindings reads the player's Elite control preset, to know which
// buttons and keys fire the actions EDSense gives a feel to, and which
// sticks turn the ship.
package bindings

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// Action is an Elite binding name.
type Action string

const (
	HeatSink   Action = "DeployHeatSink"
	Chaff      Action = "FireChaffLauncher"
	ShieldCell Action = "UseShieldCell"
	Boost      Action = "UseBoostJuice"
)

var watched = []Action{HeatSink, Chaff, ShieldCell, Boost}

// input is one physical input: a controller button or a keyboard key
// (Windows virtual-key code).
type input struct {
	button dualsense.Button
	key    int
}

func (i input) valid() bool { return i.button != 0 || i.key != 0 }

type binding struct {
	input     input
	modifiers []input
}

// Bindings are the parsed ship-controls preset.
type Bindings struct {
	Preset    string
	File      string
	actions   map[Action][]binding
	modifiers map[input]bool // inputs used as a modifier anywhere in the preset
	// TurnSticks: the stick axes (LX, LY, RX, RY) bound to yaw, pitch or roll
	TurnSticks [4]bool
}

// Has reports whether the preset binds the action.
func (b *Bindings) Has(a Action) bool { return b != nil && len(b.actions[a]) > 0 }

// Parse reads a .binds file.
func Parse(data []byte) (*Bindings, error) {
	var root struct {
		Preset  string      `xml:"PresetName,attr"`
		Entries []xmlAction `xml:",any"`
	}
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	b := &Bindings{Preset: root.Preset, actions: map[Action][]binding{}, modifiers: map[input]bool{}}
	for _, e := range root.Entries {
		name := e.XMLName.Local
		if e.Axis != nil && isRotationAxis(name) {
			if i := stickIndex(e.Axis.Device, e.Axis.Key); i >= 0 {
				b.TurnSticks[i] = true
			}
		}
		for _, k := range []*xmlKey{e.Primary, e.Secondary} {
			if k != nil {
				b.add(Action(name), k)
			}
		}
	}
	return b, nil
}

type xmlKey struct {
	Device    string   `xml:"Device,attr"`
	Key       string   `xml:"Key,attr"`
	Modifiers []xmlKey `xml:"Modifier"`
}

type xmlAction struct {
	XMLName   xml.Name
	Primary   *xmlKey `xml:"Primary"`
	Secondary *xmlKey `xml:"Secondary"`
	Axis      *xmlKey `xml:"Binding"`
}

// add records a binding; modifiers count for every action, bindings only
// for the watched ones.
func (b *Bindings) add(a Action, k *xmlKey) {
	in := inputFor(k.Device, k.Key)
	ok := in.valid()
	var mods []input
	for _, m := range k.Modifiers {
		mi := inputFor(m.Device, m.Key)
		if !mi.valid() {
			ok = false
			continue
		}
		mods = append(mods, mi)
		b.modifiers[mi] = true
	}
	if ok && isWatched(a) {
		b.actions[a] = append(b.actions[a], binding{input: in, modifiers: mods})
	}
}

func isWatched(a Action) bool {
	for _, w := range watched {
		if w == a {
			return true
		}
	}
	return false
}

func isRotationAxis(name string) bool {
	return name == "YawAxisRaw" || name == "PitchAxisRaw" || name == "RollAxisRaw"
}

// Elite's names for the DualSense / DualShock 4.
func isSonyPad(device string) bool {
	switch device {
	case "DualShock4", "DualSense", "054C05C4", "054C09CC", "054C0CE6", "054C0DF2":
		return true
	}
	return false
}

// stickIndex: which stick axis (LX, LY, RX, RY) an axis binding reads, or -1.
func stickIndex(device, key string) int {
	axes := map[string]int{}
	switch {
	case isSonyPad(device):
		axes = map[string]int{"Joy_XAxis": 0, "Joy_YAxis": 1, "Joy_ZAxis": 2, "Joy_RZAxis": 3}
	case device == "GamePad":
		axes = map[string]int{
			"GamePad_LStickX": 0, "Joy_XAxis": 0, "GamePad_LStickY": 1, "Joy_YAxis": 1,
			"GamePad_RStickX": 2, "Joy_RXAxis": 2, "GamePad_RStickY": 3, "Joy_RYAxis": 3,
		}
	}
	if i, ok := axes[key]; ok {
		return i
	}
	return -1
}

// DirectInput button order of the DualSense / DualShock 4 (Joy_1 ... Joy_14).
var padButtons = []dualsense.Button{
	dualsense.Square, dualsense.Cross, dualsense.Circle, dualsense.Triangle,
	dualsense.L1, dualsense.R1, dualsense.L2, dualsense.R2,
	dualsense.Create, dualsense.Options, dualsense.L3, dualsense.R3,
	dualsense.PS, dualsense.Touchpad,
}

var padHat = map[string]dualsense.Button{
	"Joy_POV1Up": dualsense.DpadUp, "Joy_POV1Down": dualsense.DpadDown,
	"Joy_POV1Left": dualsense.DpadLeft, "Joy_POV1Right": dualsense.DpadRight,
}

func inputFor(device, key string) input {
	switch {
	case isSonyPad(device):
		if b, ok := padHat[key]; ok {
			return input{button: b}
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(key, "Joy_")); err == nil && strings.HasPrefix(key, "Joy_") && n >= 1 && n <= len(padButtons) {
			return input{button: padButtons[n-1]}
		}
	case device == "Keyboard":
		return input{key: virtualKey(key)}
	}
	return input{}
}

var presetStartFiles = []string{"StartPreset.4.start", "StartPreset.start"}

// ShipPreset finds the ship-controls preset from StartPreset.*.start
// (Odyssey writes four lines: general, ship, SRV, on foot; older versions
// one) and its newest "<preset>.<major>.<minor>.binds". file is "" for a
// built-in preset, which lives in the game folder and is not read.
func ShipPreset(dir string) (preset, file string) {
	var start string
	for _, n := range presetStartFiles {
		if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
			start = string(b)
			break
		}
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(start, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	switch len(lines) {
	case 0:
		return "", ""
	case 1:
		preset = lines[0]
	default:
		preset = lines[1]
	}
	return preset, newestBindsFile(dir, preset)
}

func newestBindsFile(dir, preset string) string {
	versioned := regexp.MustCompile(`^` + regexp.QuoteMeta(preset) + `\.(\d+)\.(\d+)\.binds$`)
	type candidate struct {
		name         string
		major, minor int
	}
	var list []candidate
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if m := versioned.FindStringSubmatch(e.Name()); m != nil {
			major, _ := strconv.Atoi(m[1])
			minor, _ := strconv.Atoi(m[2])
			list = append(list, candidate{e.Name(), major, minor})
		}
	}
	sort.Slice(list, func(i, k int) bool {
		if list[i].major != list[k].major {
			return list[i].major > list[k].major
		}
		return list[i].minor > list[k].minor
	})
	if len(list) > 0 {
		return filepath.Join(dir, list[0].name)
	}
	if plain := filepath.Join(dir, preset+".binds"); fileExists(plain) {
		return plain
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
