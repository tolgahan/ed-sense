package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Key is one setting as the window shows it: its type, range and page.
// A path ending in ".*" is a map: Keys are its keys, and the type, range
// and enum are each entry's.
type Key struct {
	Path     string   `json:"path"`           // "lightbar_brightness", "haptics_gain.*"
	Keys     []string `json:"keys,omitempty"` // for "*": the keys a map takes (fire_groups: any of Min-Max)
	Type     string   `json:"type"`           // bool, int, float, enum, path, rgb, hex, trigger, rumble, firegroup, intset
	Default  any      `json:"default"`        // from Default() through JSON
	Min      float64  `json:"min,omitempty"`
	Max      float64  `json:"max,omitempty"`
	Step     float64  `json:"step,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Scale    float64  `json:"scale,omitempty"` // shown as value x Scale
	Log      bool     `json:"log,omitempty"`   // a slider on a log scale
	Restart  string   `json:"restart,omitempty"`
	Needs    string   `json:"needs,omitempty"` // a backend cap (triggers, lightbar, player_leds, mic, haptics, gyro, hud), or "ds4windows"
	Page     string   `json:"page"`            // the window page it is on
	Editable bool     `json:"editable"`        // settings.patch takes it
}

// What a change to a restart key needs before it applies.
const (
	RestartSession = "session" // a new session on the same backend
	RestartBackend = "backend" // a new backend
)

const (
	typeBool      = "bool"
	typeInt       = "int"
	typeFloat     = "float"
	typeEnum      = "enum"
	typePath      = "path"
	typeRGB       = "rgb"       // [r, g, b], each Min-Max
	typeHex       = "hex"       // "#rrggbb" or ""; flash also "off"
	typeTrigger   = "trigger"   // {mode, params}, by triggerModes
	typeRumble    = "rumble"    // {left, right: Min-Max; ms: 0-rumbleMaxMs}
	typeFireGroup = "firegroup" // {primary, secondary} in Enum; the keys are Min-Max
	typeIntSet    = "intset"    // whole numbers Min-Max, none twice
)

// rumbleMaxMs is the longest one-shot rumble a patch sets.
const rumbleMaxMs = 10000

// hudColorKeys are the HUD colours a player can set.
var hudColorKeys = []string{"shield", "heat", "flame", "flash", "hull"}

// weaponClasses are what a fire group's trigger can be set to.
var weaponClasses = []string{"auto", "beam", "pulse", "burst", "multicannon", "cannon", "fragment", "railgun", "plasma", "missile", "mining", "generic"}

// Schema lists every setting, in the file's order.
func Schema() []Key {
	doc := defaultDoc()
	keys := rows()
	for i := range keys {
		keys[i].Default = doc[keyName(keys[i])]
	}
	return keys
}

// rows is the Schema without the defaults.
func rows() []Key {
	d := Default()
	pct := func(k Key) Key { k.Unit, k.Scale = "%", 100; return k }
	return []Key{
		{Path: "config_version", Type: typeInt, Min: 1, Max: Version, Page: "advanced"}, // a newer file keeps its own
		{Path: "backend", Type: typeEnum, Enum: []string{"", BackendAuto, BackendDSX, BackendDS4Windows}, Restart: RestartBackend, Page: "controller"},
		{Path: "journal_dir", Type: typePath, Restart: RestartSession, Page: "advanced"},
		{Path: "bindings_dir", Type: typePath, Restart: RestartSession, Page: "advanced"},
		{Path: "dsx_port", Type: typeInt, Max: 65535, Restart: RestartBackend, Page: "controller", Editable: true},
		{Path: "ds4windows_port", Type: typeInt, Max: 65535, Restart: RestartBackend, Page: "controller", Editable: true},
		{Path: "poll_ms", Type: typeInt, Min: 20, Max: 100, Unit: "ms", Restart: RestartSession, Page: "advanced", Editable: true},
		{Path: "control_lightbar", Type: typeBool, Needs: "lightbar", Page: "lights", Editable: true},
		{Path: "control_triggers", Type: typeBool, Needs: "triggers", Page: "triggers", Editable: true},
		{Path: "control_player_leds", Type: typeBool, Needs: "player_leds", Page: "lights", Editable: true},
		{Path: "control_mic_led", Type: typeBool, Needs: "mic", Page: "lights", Editable: true},
		{Path: "control_haptics", Type: typeBool, Page: "feel", Editable: true},
		pct(Key{Path: "haptics_strength", Type: typeFloat, Max: 3, Step: 0.05, Page: "feel", Editable: true}),
		{Path: "haptics_mode", Type: typeEnum, Enum: []string{HapticsAuto, HapticsNative, HapticsRumble}, Page: "feel", Editable: true},
		{Path: "ds4windows_haptics", Type: typeEnum, Enum: []string{DS4WHapticsAuto, DS4WHapticsController, DS4WHapticsVirtual}, Needs: "ds4windows", Page: "controller", Editable: true},
		pct(Key{Path: "haptics_gain.*", Keys: sortedKeys(d.HapticsGain), Type: typeFloat, Max: 2, Step: 0.05, Page: "feel", Editable: true}),
		{Path: "turn_feel", Type: typeEnum, Enum: []string{TurnWaves, TurnPush, TurnOff}, Page: "feel", Editable: true},
		{Path: "jump_feel", Type: typeEnum, Enum: []string{JumpSwell, JumpCalm, JumpOff}, Page: "feel", Editable: true},
		{Path: "fire_groups.*", Type: typeFireGroup, Min: 1, Max: 99, Enum: slices.Clone(weaponClasses), Page: "feel", Editable: true},
		{Path: "spin_up_ms.*", Keys: sortedKeys(d.SpinUpMs), Type: typeInt, Max: 5000, Step: 50, Unit: "ms", Page: "feel", Editable: true},
		{Path: "gyro_aim", Type: typeBool, Needs: "gyro", Page: "gyro", Editable: true},
		{Path: "gyro_off_in_menus", Type: typeBool, Needs: "gyro", Page: "gyro", Editable: true},
		{Path: "gyro_by", Type: typeEnum, Enum: []string{GyroByEDSense, GyroByDSX}, Page: "gyro", Editable: true},
		{Path: "gyro_sensitivity_x", Type: typeFloat, Min: 0.05, Max: 20, Log: true, Page: "gyro", Editable: true},
		{Path: "gyro_sensitivity_y", Type: typeFloat, Min: 0.05, Max: 20, Log: true, Page: "gyro", Editable: true},
		pct(Key{Path: "gyro_roll_mix", Type: typeFloat, Max: 2, Step: 0.05, Page: "gyro", Editable: true}),
		{Path: "gyro_low_speed", Type: typeEnum, Enum: []string{GyroLowDSX, GyroLowExact}, Page: "gyro", Editable: true},
		{Path: "gyro_auto_calibrate", Type: typeBool, Needs: "gyro", Page: "gyro", Editable: true},
		{Path: "gyro_off_gui_focus", Type: typeIntSet, Min: 1, Max: 11, Page: "gyro", Editable: true},
		{Path: "hud_reader", Type: typeBool, Needs: "hud", Page: "hud", Editable: true},
		{Path: "hud_debug", Type: typeBool, Page: "hud"}, // it saves screen captures
		{Path: "hud_colors.*", Keys: slices.Clone(hudColorKeys), Type: typeHex, Page: "hud", Editable: true},
		{Path: "lightbar_brightness", Type: typeInt, Max: 255, Unit: "%", Scale: 100.0 / 255, Page: "lights", Editable: true},
		{Path: "colors.*", Keys: sortedKeys(d.Colors), Type: typeRGB, Max: 255, Page: "lights", Editable: true},
		{Path: "triggers.*", Keys: sortedKeys(d.TriggerFX), Type: typeTrigger, Enum: triggerModeNames(), Page: "triggers", Editable: true},
		{Path: "rumble.*", Keys: sortedKeys(d.Rumble), Type: typeRumble, Max: 1, Step: 0.01, Page: "advanced", Editable: true},
	}
}

// keyName is the setting's name in the file: the path without ".*".
func keyName(k Key) string { return strings.TrimSuffix(k.Path, ".*") }

func isMap(k Key) bool { return strings.HasSuffix(k.Path, ".*") }

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

// schemaIndex finds a row by the setting's name in the file.
var schemaIndex = sync.OnceValue(func() map[string]Key {
	m := map[string]Key{}
	for _, k := range rows() {
		m[keyName(k)] = k
	}
	return m
})

// defaultDoc is Default() as JSON values: objects, lists, json.Number,
// strings and bools.
func defaultDoc() map[string]any {
	doc, err := configDoc(Default())
	if err != nil {
		panic(err) // the defaults always marshal
	}
	return doc
}

// configDoc is cfg as JSON values.
func configDoc(cfg Config) (map[string]any, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	v, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	doc, _ := v.(map[string]any)
	return doc, nil
}

// decodeJSON reads one JSON value with its numbers as written.
func decodeJSON(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("more than one JSON value")
	}
	return v, nil
}

// A trigger mode both DSX and DS4Windows take, with each value's range.
// above: the value at this index must be above the one before it (the end
// above the start); 0 when no value must.
type triggerMode struct {
	name   string
	params [][2]int
	above  int
}

func repeat(n int, r [2]int) [][2]int {
	out := make([][2]int, n)
	for i := range out {
		out[i] = r
	}
	return out
}

// triggerModes: internal/dsx/frame.go, within what DS4Windows takes.
var triggerModes = []triggerMode{
	{name: "OFF"},
	{name: "FEEDBACK", params: [][2]int{{1, 9}, {1, 8}}},                                 // start, strength
	{name: "WEAPON", params: [][2]int{{2, 7}, {3, 8}, {1, 8}}, above: 1},                 // start, end, strength
	{name: "VIBRATION", params: [][2]int{{1, 9}, {1, 8}, {1, 40}}},                       // start, amplitude, frequency
	{name: "SLOPE_FEEDBACK", params: [][2]int{{1, 8}, {2, 9}, {1, 8}, {1, 8}}, above: 1}, // start, end, two strengths
	{name: "MULTIPLE_POSITION_FEEDBACK", params: repeat(10, [2]int{0, 8})},
	{name: "MULTIPLE_POSITION_VIBRATION", params: append([][2]int{{1, 40}}, repeat(10, [2]int{0, 8})...)}, // frequency, then 10 amplitudes
}

func triggerModeNames() []string {
	names := make([]string, len(triggerModes))
	for i, m := range triggerModes {
		names[i] = m.name
	}
	return names
}

// checker collects the problems of a patch.
type checker struct {
	problems []Problem
}

func (c *checker) add(path, code, msg string) {
	c.problems = append(c.problems, Problem{Path: path, Code: code, Msg: msg})
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func rangeMsg(lo, hi float64) string { return "from " + num(lo) + " to " + num(hi) }

// whole checks a whole number within lo and hi.
func (c *checker) whole(path string, v any, lo, hi float64) (int, bool) {
	n, ok := v.(json.Number)
	if !ok {
		c.add(path, CodeType, "should be a whole number")
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	switch {
	case errors.Is(err, strconv.ErrRange):
		c.add(path, CodeRange, rangeMsg(lo, hi))
		return 0, false
	case err != nil:
		c.add(path, CodeType, "should be a whole number")
		return 0, false
	case float64(i) < lo || float64(i) > hi:
		c.add(path, CodeRange, rangeMsg(lo, hi))
		return 0, false
	}
	return int(i), true
}

// number checks a number within lo and hi.
func (c *checker) number(path string, v any, lo, hi float64) {
	n, ok := v.(json.Number)
	if !ok {
		c.add(path, CodeType, "should be a number")
		return
	}
	if f, err := n.Float64(); err != nil || f < lo || f > hi {
		c.add(path, CodeRange, rangeMsg(lo, hi))
	}
}

// word checks a string from enum.
func (c *checker) word(path string, v any, enum []string) {
	s, ok := v.(string)
	switch {
	case !ok:
		c.add(path, CodeType, "should be text")
	case !slices.Contains(enum, s):
		var named []string
		for _, e := range enum {
			if e != "" {
				named = append(named, e)
			}
		}
		c.add(path, CodeEnum, "one of "+strings.Join(named, ", "))
	}
}

// object checks a JSON object whose fields are all in fields, and returns
// it (nil for anything else). A null field is a type problem: only a whole
// setting or a whole map entry goes back to its default.
func (c *checker) object(path string, v any, fields ...string) map[string]any {
	o, ok := v.(map[string]any)
	if !ok {
		c.add(path, CodeType, "should be an object")
		return nil
	}
	for _, f := range sortedKeys(o) {
		switch {
		case !slices.Contains(fields, f):
			c.add(path+"."+f, CodeUnknown, "not a setting")
		case o[f] == nil:
			c.add(path+"."+f, CodeType, "cannot be null")
		}
	}
	return o
}

// value checks a whole setting that is not a map.
func (c *checker) value(k Key, path string, v any) {
	switch k.Type {
	case typeBool:
		if _, ok := v.(bool); !ok {
			c.add(path, CodeType, "should be true or false")
		}
	case typeInt:
		c.whole(path, v, k.Min, k.Max)
	case typeFloat:
		c.number(path, v, k.Min, k.Max)
	case typeEnum:
		c.word(path, v, k.Enum)
	case typePath:
		if _, ok := v.(string); !ok {
			c.add(path, CodeType, "should be text")
		}
	case typeIntSet:
		list, ok := v.([]any)
		if !ok {
			c.add(path, CodeType, "should be a list")
			return
		}
		seen := map[int]bool{}
		for i, e := range list {
			at := path + "." + strconv.Itoa(i)
			n, ok := c.whole(at, e, k.Min, k.Max)
			if ok && seen[n] {
				c.add(at, CodeRange, "listed twice")
			}
			seen[n] = true
		}
	default:
		c.add(path, CodeType, "not a single value")
	}
}

// entryName checks the key of one map entry.
func (c *checker) entryName(k Key, path, name string) bool {
	if k.Type == typeFireGroup {
		n, err := strconv.Atoi(name)
		switch {
		case err != nil || strconv.Itoa(n) != name:
			c.add(path, CodeUnknown, "a fire group is a number")
			return false
		case float64(n) < k.Min || float64(n) > k.Max:
			c.add(path, CodeRange, "a fire group from "+num(k.Min)+" to "+num(k.Max))
			return false
		}
		return true
	}
	if !slices.Contains(k.Keys, name) {
		c.add(path, CodeUnknown, "not a setting")
		return false
	}
	return true
}

// entry checks one map entry's value. A trigger's values are checked
// against its mode once merged (trigger).
func (c *checker) entry(k Key, path, name string, v any) {
	switch k.Type {
	case typeInt:
		c.whole(path, v, k.Min, k.Max)
	case typeFloat:
		c.number(path, v, k.Min, k.Max)
	case typeRGB:
		list, ok := v.([]any)
		if !ok {
			c.add(path, CodeType, "should be a list of 3 numbers")
			return
		}
		if len(list) != 3 {
			c.add(path, CodeCount, "needs 3 values")
			return
		}
		for i, e := range list {
			c.whole(path+"."+strconv.Itoa(i), e, k.Min, k.Max)
		}
	case typeHex:
		s, ok := v.(string)
		switch {
		case !ok:
			c.add(path, CodeType, "should be text")
		case s == "", name == "flash" && s == "off", isHexColor(s):
		default:
			c.add(path, CodeType, "a colour as #rrggbb")
		}
	case typeFireGroup:
		o := c.object(path, v, "primary", "secondary")
		for _, f := range []string{"primary", "secondary"} {
			if e := o[f]; e != nil {
				c.word(path+"."+f, e, k.Enum)
			}
		}
	case typeRumble:
		o := c.object(path, v, "left", "right", "ms")
		for _, f := range []string{"left", "right"} {
			if e := o[f]; e != nil {
				c.number(path+"."+f, e, k.Min, k.Max)
			}
		}
		if e := o["ms"]; e != nil {
			c.whole(path+".ms", e, 0, rumbleMaxMs)
		}
	case typeTrigger:
		o := c.object(path, v, "mode", "params")
		if m := o["mode"]; m != nil {
			c.word(path+".mode", m, k.Enum)
		}
		if p := o["params"]; p != nil {
			list, ok := p.([]any)
			if !ok {
				c.add(path+".params", CodeType, "should be a list")
				return
			}
			// the ranges depend on the mode, so they are checked once merged
			for i, e := range list {
				c.whole(path+".params."+strconv.Itoa(i), e, math.MinInt32, math.MaxInt32)
			}
		}
	default:
		c.add(path, CodeType, "not a map entry")
	}
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 16, 32)
	return err == nil
}

// trigger checks a whole trigger: its mode, the number of values and each
// value's range.
func (c *checker) trigger(path string, t Trigger) {
	i := slices.IndexFunc(triggerModes, func(m triggerMode) bool { return m.name == t.Mode })
	if i < 0 {
		c.add(path+".mode", CodeEnum, "one of "+strings.Join(triggerModeNames(), ", "))
		return
	}
	m := triggerModes[i]
	if len(t.Params) != len(m.params) {
		c.add(path+".params", CodeCount, fmt.Sprintf("%s takes %d values", m.name, len(m.params)))
		return
	}
	for i, p := range t.Params {
		lo, hi := m.params[i][0], m.params[i][1]
		if prev := i - 1; m.above > 0 && i == m.above && t.Params[prev] >= m.params[prev][0] && t.Params[prev] <= m.params[prev][1] {
			lo = max(lo, t.Params[prev]+1)
		}
		if p < lo || p > hi {
			c.add(path+".params."+strconv.Itoa(i), CodeRange, rangeMsg(float64(lo), float64(hi)))
		}
	}
}
