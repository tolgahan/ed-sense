package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/control"
)

// problemsText is problems as "path code" lines, for comparing.
func problemsText(ps []Problem) string {
	var out []string
	for _, p := range ps {
		if p.Msg == "" {
			out = append(out, p.Path+" "+p.Code+" (no message)")
			continue
		}
		out = append(out, p.Path+" "+p.Code)
	}
	return strings.Join(out, "; ")
}

// TestPatchProblems: each problem code, and that a refused patch returns
// cur as it was.
func TestPatchProblems(t *testing.T) {
	for _, c := range []struct{ patch, want string }{
		// unknown: paths are exact case
		{`{"nope": 1}`, "nope unknown"},
		{`{"Poll_ms": 30}`, "Poll_ms unknown"},
		{`{"haptics_gain": {"danger": 1}}`, "haptics_gain.danger unknown"},
		{`{"hud_colors": {"main": "#ffffff"}}`, "hud_colors.main unknown"},
		{`{"fire_groups": {"one": {"primary": "beam"}}}`, "fire_groups.one unknown"},
		{`{"fire_groups": {"01": {"primary": "beam"}}}`, "fire_groups.01 unknown"},
		{`{"triggers": {"hit": {"mode": "OFF", "params": [], "strength": 1}}}`, "triggers.hit.strength unknown"},
		{`{"rumble": {"boost": {"left": 1, "Ms": 10}}}`, "rumble.boost.Ms unknown"},
		// readonly
		{`{"backend": "dsx"}`, "backend readonly"},
		{`{"config_version": 5}`, "config_version readonly"},
		{`{"backend": null}`, "backend readonly"},
		{`{"config_version": null}`, "config_version readonly"},
		// type
		{`{"control_triggers": 1}`, "control_triggers type"},
		{`{"poll_ms": "30"}`, "poll_ms type"},
		{`{"poll_ms": 30.5}`, "poll_ms type"},
		{`{"poll_ms": 3e1}`, "poll_ms type"},
		{`{"haptics_strength": true}`, "haptics_strength type"},
		{`{"haptics_mode": 1}`, "haptics_mode type"},
		{`{"colors": [1, 2, 3]}`, "colors type"},
		{`{"colors": {"hit": "red"}}`, "colors.hit type"},
		{`{"colors": {"hit": [1, 2.5, 3]}}`, "colors.hit.1 type"},
		{`{"hud_colors": {"heat": "red"}}`, "hud_colors.heat type"},
		{`{"hud_colors": {"heat": "off"}}`, "hud_colors.heat type"},
		{`{"gyro_off_gui_focus": 6}`, "gyro_off_gui_focus type"},
		{`{"gyro_off_gui_focus": [6, null]}`, "gyro_off_gui_focus.1 type"},
		{`{"triggers": {"hit": "OFF"}}`, "triggers.hit type"},
		{`{"triggers": {"hit": {"params": [[1]]}}}`, "triggers.hit.params.0 type"},
		{`{"triggers": {"hit": {"params": {"0": 1}}}}`, "triggers.hit.params type"},
		{`{"hud_debug": "yes"}`, "hud_debug type"},
		// a folder: "" or a full path
		{`{"journal_dir": 5}`, "journal_dir type"},
		{`{"journal_dir": null, "bindings_dir": ["C:\\x"]}`, "bindings_dir type"},
		{`{"journal_dir": "Elite\\Journal"}`, "journal_dir type"},
		{`{"journal_dir": "\\Elite\\Journal"}`, "journal_dir type"},
		{`{"journal_dir": "D:Elite"}`, "journal_dir type"},
		{`{"bindings_dir": " C:\\x"}`, "bindings_dir type"},
		{`{"bindings_dir": "C:\\x\t"}`, "bindings_dir type"},
		{`{"journal_dir": "C:\\a|b"}`, "journal_dir type"},
		{`{"journal_dir": "C:\\a\u0001b"}`, "journal_dir type"},
		{`{"journal_dir": "C:\\a?"}`, "journal_dir type"},
		{`{"journal_dir": "\"C:\\a\""}`, "journal_dir type"},
		// a null deeper than a whole map entry
		{`{"rumble": {"boost": {"ms": null}}}`, "rumble.boost.ms type"},
		{`{"triggers": {"hit": {"mode": null}}}`, "triggers.hit.mode type"},
		{`{"triggers": {"hit": {"mode": "OFF", "params": null}}}`, "triggers.hit.params type"},
		{`{"fire_groups": {"1": {"primary": null}}}`, "fire_groups.1.primary type"},
		// range
		{`{"poll_ms": 19}`, "poll_ms range"},
		{`{"poll_ms": 101}`, "poll_ms range"},
		{`{"journal_dir": "C:\\` + strings.Repeat("x", 1022) + `"}`, "journal_dir range"}, // 1025 bytes
		{`{"dsx_port": 70000}`, "dsx_port range"},
		{`{"ds4windows_port": -1}`, "ds4windows_port range"},
		{`{"dsx_port": 99999999999999999999}`, "dsx_port range"},
		{`{"haptics_strength": 3.01}`, "haptics_strength range"},
		{`{"gyro_sensitivity_x": 0.01}`, "gyro_sensitivity_x range"},
		{`{"lightbar_brightness": 256}`, "lightbar_brightness range"},
		{`{"haptics_gain": {"boost": 2.5}}`, "haptics_gain.boost range"},
		{`{"spin_up_ms": {"huge": 5001}}`, "spin_up_ms.huge range"},
		{`{"colors": {"hit": [300, 0, -1]}}`, "colors.hit.0 range; colors.hit.2 range"},
		{`{"rumble": {"boost": {"left": 1.5, "ms": 10001}}}`, "rumble.boost.left range; rumble.boost.ms range"},
		{`{"gyro_off_gui_focus": [0, 12]}`, "gyro_off_gui_focus.0 range; gyro_off_gui_focus.1 range"},
		{`{"gyro_off_gui_focus": [6, 7, 6]}`, "gyro_off_gui_focus.2 range"},
		{`{"fire_groups": {"100": {"primary": "beam"}}}`, "fire_groups.100 range"},
		{`{"fire_groups": {"0": {"primary": "beam"}}}`, "fire_groups.0 range"},
		{`{"triggers": {"hit": {"mode": "VIBRATION", "params": [1, 5, 41]}}}`, "triggers.hit.params.2 range"},
		{`{"triggers": {"hit": {"mode": "WEAPON", "params": [5, 5, 6]}}}`, "triggers.hit.params.1 range"},
		{`{"triggers": {"hit": {"mode": "WEAPON", "params": [8, 9, 6]}}}`, "triggers.hit.params.0 range; triggers.hit.params.1 range"},
		{`{"triggers": {"hit": {"mode": "SLOPE_FEEDBACK", "params": [3, 3, 1, 8]}}}`, "triggers.hit.params.1 range"},
		{`{"triggers": {"hit": {"mode": "MULTIPLE_POSITION_VIBRATION", "params": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9]}}}`, "triggers.hit.params.0 range; triggers.hit.params.10 range"},
		// enum
		{`{"haptics_mode": "loud"}`, "haptics_mode enum"},
		{`{"haptics_mode": "AUTO"}`, "haptics_mode enum"},
		{`{"ds4windows_haptics": ""}`, "ds4windows_haptics enum"},
		{`{"triggers": {"hit": {"mode": "weapon", "params": [2, 5, 6]}}}`, "triggers.hit.mode enum"},
		{`{"fire_groups": {"3": {"primary": "laser"}}}`, "fire_groups.3.primary enum"},
		// count
		{`{"colors": {"hit": [1, 2]}}`, "colors.hit count"},
		{`{"triggers": {"hit": {"mode": "OFF"}}}`, "triggers.hit.params count"}, // the old params stay
		{`{"triggers": {"hit": {"mode": "WEAPON", "params": [2, 5]}}}`, "triggers.hit.params count"},
		// several at once, in path order
		{`{"poll_ms": 5, "backend": "dsx", "nope": true}`, "backend readonly; nope unknown; poll_ms range"},
	} {
		cur := Default()
		before, _ := json.Marshal(cur)
		next, changed, ps := MergePatch(cur, []byte(c.patch))
		if got := problemsText(ps); got != c.want {
			t.Errorf("%s: %s, want %s", c.patch, got, c.want)
		}
		if after, _ := json.Marshal(cur); string(after) != string(before) {
			t.Errorf("%s: cur was changed", c.patch)
		}
		if b, _ := json.Marshal(next); string(b) != string(before) || changed != nil {
			t.Errorf("%s: refused, but returned changes %v", c.patch, changed)
		}
	}
}

// TestPatchNotAnObject: a patch must be one JSON object.
func TestPatchNotAnObject(t *testing.T) {
	for _, patch := range []string{``, `null`, `[]`, `1`, `"x"`, `{`, `{} {}`, `{"poll_ms": 30} x`} {
		_, _, ps := MergePatch(Default(), []byte(patch))
		if got := problemsText(ps); got != " type" {
			t.Errorf("%q: %s", patch, got)
		}
	}
}

// TestPatchApplies: a good patch sets what it names, and changed lists it.
func TestPatchApplies(t *testing.T) {
	cur := Default()
	next, changed, ps := MergePatch(cur, []byte(`{
		"poll_ms": 30, "control_triggers": false, "haptics_mode": "rumble", "dsx_port": 0,
		"colors": {"hit": [1, 2, 3]},
		"haptics_gain": {"boost": 0.5, "fire_primary": 1},
		"triggers": {"hit": {"mode": "WEAPON", "params": [2, 5, 6]}, "jet_cone": {"mode": "OFF", "params": []}},
		"rumble": {"boost": {"ms": 100}},
		"fire_groups": {"3": {"primary": "beam", "secondary": "missile"}},
		"hud_colors": {"flash": "off", "heat": "#FFaa00"},
		"gyro_off_gui_focus": [6, 7]
	}`))
	if len(ps) > 0 {
		t.Fatalf("problems: %s", problemsText(ps))
	}
	want := []string{"poll_ms", "control_triggers", "haptics_mode", "haptics_gain.boost", "fire_groups.3",
		"gyro_off_gui_focus", "hud_colors.flash", "hud_colors.heat", "colors.hit", "triggers.hit", "triggers.jet_cone", "rumble.boost"}
	if !slices.Equal(changed, want) {
		t.Errorf("changed %v\nwant    %v", changed, want)
	}
	if next.PollMs != 30 || next.Triggers || next.HapticsMode != HapticsRumble || next.Colors["hit"] != [3]int{1, 2, 3} ||
		next.Gain("boost") != 0.5 || !reflect.DeepEqual(next.TriggerFX["hit"], Trigger{"WEAPON", []int{2, 5, 6}}) ||
		next.TriggerFX["jet_cone"].Mode != "OFF" || len(next.TriggerFX["jet_cone"].Params) != 0 ||
		next.Rumble["boost"] != (Rumble{0.7, 0.7, 100}) || next.FireGroups["3"] != (FireGroup{"beam", "missile"}) ||
		next.FireGroups["1"] != (FireGroup{"auto", "auto"}) || next.HUDColors["flash"] != "off" || next.HUDColors["heat"] != "#FFaa00" ||
		!slices.Equal(next.GyroOffGuiFocus, []int{6, 7}) {
		t.Errorf("next: %+v", next)
	}
	if cur.Colors["hit"] != Default().Colors["hit"] || cur.TriggerFX["hit"].Mode != "VIBRATION" || len(cur.FireGroups) != 2 {
		t.Error("cur was changed")
	}
	// the maps are new: changing next leaves cur alone
	next.Colors["kill"] = [3]int{}
	if cur.Colors["kill"] != Default().Colors["kill"] {
		t.Error("next shares a map with cur")
	}

	same, changed, ps := MergePatch(next, []byte(`{"poll_ms": 30, "triggers": {"hit": {"mode": "WEAPON"}}}`))
	if len(ps) > 0 || len(changed) != 0 || same.PollMs != 30 {
		t.Errorf("no change: %v %s", changed, problemsText(ps))
	}
	if _, changed, ps := MergePatch(next, []byte(`{}`)); len(ps) > 0 || len(changed) != 0 {
		t.Errorf("empty patch: %v %s", changed, problemsText(ps))
	}
}

// TestPatchNull: null puts a whole setting or a whole map entry back to its
// default; an entry the defaults lack goes.
func TestPatchNull(t *testing.T) {
	cur := Default()
	cur.PollMs, cur.Lightbar, cur.HapticsMode = 50, false, HapticsNative
	cur.Colors["hit"] = [3]int{1, 1, 1}
	cur.TriggerFX["hit"] = Trigger{"OFF", nil}
	cur.FireGroups["1"] = FireGroup{"beam", "beam"}
	cur.FireGroups["3"] = FireGroup{"rail", "rail"}
	cur.HUDColors["heat"] = "#ff0000"
	cur.GyroOffGuiFocus = []int{6}
	cur.Rumble["boost"] = Rumble{0, 0, 1}
	next, changed, ps := MergePatch(cur, []byte(`{
		"poll_ms": null, "control_lightbar": null, "haptics_mode": null,
		"colors": {"hit": null}, "triggers": null, "fire_groups": {"1": null, "3": null},
		"hud_colors": null, "gyro_off_gui_focus": null, "rumble": {"boost": null}
	}`))
	if len(ps) > 0 {
		t.Fatalf("problems: %s", problemsText(ps))
	}
	d := Default()
	if next.PollMs != d.PollMs || !next.Lightbar || next.HapticsMode != d.HapticsMode || next.Colors["hit"] != d.Colors["hit"] ||
		!reflect.DeepEqual(next.TriggerFX, d.TriggerFX) || !reflect.DeepEqual(next.FireGroups, d.FireGroups) ||
		len(next.HUDColors) != 0 || !slices.Equal(next.GyroOffGuiFocus, d.GyroOffGuiFocus) || next.Rumble["boost"] != d.Rumble["boost"] {
		t.Errorf("not back to the defaults: %+v", next)
	}
	want := []string{"poll_ms", "control_lightbar", "haptics_mode", "fire_groups.1", "fire_groups.3", "gyro_off_gui_focus",
		"hud_colors.heat", "colors.hit", "triggers.hit", "rumble.boost"}
	if !slices.Equal(changed, want) {
		t.Errorf("changed %v\nwant    %v", changed, want)
	}
}

// TestPatchArraysReplaced: a list in a patch replaces the whole list.
func TestPatchArraysReplaced(t *testing.T) {
	cur := Default()
	cur.TriggerFX["hit"] = Trigger{"MULTIPLE_POSITION_FEEDBACK", []int{1, 2, 3, 4, 5, 6, 7, 8, 8, 8}}
	next, _, ps := MergePatch(cur, []byte(`{"gyro_off_gui_focus": [11], "triggers": {"hit": {"mode": "FEEDBACK", "params": [1, 2]}}}`))
	if len(ps) > 0 || !slices.Equal(next.GyroOffGuiFocus, []int{11}) || !slices.Equal(next.TriggerFX["hit"].Params, []int{1, 2}) {
		t.Errorf("%v %v %s", next.GyroOffGuiFocus, next.TriggerFX["hit"], problemsText(ps))
	}
	next, _, ps = MergePatch(cur, []byte(`{"gyro_off_gui_focus": []}`))
	if len(ps) > 0 || next.GyroOffGuiFocus == nil || len(next.GyroOffGuiFocus) != 0 {
		t.Errorf("empty list: %v %s", next.GyroOffGuiFocus, problemsText(ps))
	}
}

// TestPatchKeepsHandTuned: values outside the strict ranges that a patch
// does not change are kept, and the file's own config_version stays.
func TestPatchKeepsHandTuned(t *testing.T) {
	cur := Default()
	cur.Version = 9 // a newer file
	cur.TriggerFX["hit"] = Trigger{"VIBRATION", []int{1, 5, 60}}
	cur.Colors["hull_full"] = [3]int{300, -20, 60}
	cur.HapticsGain["boost"] = 7
	cur.FireGroups["1"] = FireGroup{"", "auto"}
	next, changed, ps := MergePatch(cur, []byte(`{"poll_ms": 30}`))
	if len(ps) > 0 || !slices.Equal(changed, []string{"poll_ms"}) {
		t.Fatalf("%v %s", changed, problemsText(ps))
	}
	if next.Version != 9 || !slices.Equal(next.TriggerFX["hit"].Params, []int{1, 5, 60}) || next.Colors["hull_full"] != [3]int{300, -20, 60} ||
		next.Gain("boost") != 7 || next.FireGroups["1"] != (FireGroup{"", "auto"}) {
		t.Errorf("hand-tuned values lost: %+v", next)
	}
	// a patch that changes the trigger checks it whole
	_, _, ps = MergePatch(cur, []byte(`{"triggers": {"hit": {"mode": "VIBRATION", "params": [1, 6, 60]}}}`))
	if got := problemsText(ps); got != "triggers.hit.params.2 range" {
		t.Errorf("changed trigger: %s", got)
	}
	// the same value again is no change, so nothing to refuse
	_, changed, ps = MergePatch(cur, []byte(`{"triggers": {"hit": {"mode": "VIBRATION", "params": [1, 5, 60]}}}`))
	if len(ps) > 0 || len(changed) != 0 {
		t.Errorf("unchanged trigger: %v %s", changed, problemsText(ps))
	}
}

// TestPatchEveryTriggerMode: each mode at both ends of its ranges.
func TestPatchEveryTriggerMode(t *testing.T) {
	for _, m := range triggerModes {
		for _, end := range []int{0, 1} {
			params := make([]int, len(m.Params))
			for i, r := range m.Params {
				params[i] = r[end]
			}
			if m.Above > 0 && end == 0 {
				params[m.Above] = params[m.Above-1] + 1
			}
			b, _ := json.Marshal(params)
			patch := fmt.Sprintf(`{"triggers": {"hit": {"mode": %q, "params": %s}}}`, m.Name, b)
			next, _, ps := MergePatch(Default(), []byte(patch))
			if len(ps) > 0 || !slices.Equal(next.TriggerFX["hit"].Params, params) {
				t.Errorf("%s: %s", patch, problemsText(ps))
			}
		}
	}
}

// TestPatchFolderPaths: a folder setting takes a full path or "" (Elite's
// standard folder), and MergePatch never looks at the disk: the Store
// does (TestPatchFolders). hud_debug is set as any switch.
func TestPatchFolderPaths(t *testing.T) {
	full := filepath.Join(t.TempDir(), "Journal") // not there
	long := full + strings.Repeat("x", maxFolderLen-len(full))
	cur := Default()
	cur.BindingsDir = full
	for _, c := range []struct {
		name  string
		value any
		want  []string
	}{
		{"journal_dir", full, []string{"journal_dir"}},
		{"journal_dir", long, []string{"journal_dir"}},
		{"bindings_dir", "", []string{"bindings_dir"}},
		{"journal_dir", "", nil}, // no change
		{"hud_debug", true, []string{"hud_debug"}},
	} {
		patch, _ := json.Marshal(map[string]any{c.name: c.value})
		next, changed, ps := MergePatch(cur, patch)
		doc, _ := configDoc(next)
		if len(ps) > 0 || !slices.Equal(changed, c.want) || doc[c.name] != c.value {
			t.Errorf("%s: changed %v, %v, %s", patch, changed, doc[c.name], problemsText(ps))
		}
	}
}

// TestPatchResetAll: Reset all settings sends null for every editable
// row. It puts a changed file back to Default(), and keeps the file's
// backend and config_version, which are set elsewhere.
func TestPatchResetAll(t *testing.T) {
	cur := Default()
	cur.Version, cur.Backend = Version+1, BackendDSX
	cur.JournalDir, cur.BindingsDir, cur.DSXPort, cur.DS4WindowsPort, cur.PollMs = `D:\Elite`, `D:\Bindings`, 7000, 7001, 50
	cur.Lightbar, cur.Haptics, cur.HapticsStrength, cur.HapticsMode = false, false, 2, HapticsNative
	cur.DS4WindowsHaptics, cur.TurnFeel, cur.JumpFeel = DS4WHapticsVirtual, TurnPush, JumpCalm
	cur.GyroAim, cur.GyroBy, cur.GyroSensitivityX, cur.GyroLowSpeed = false, GyroByDSX, 3, GyroLowExact
	cur.HUDReader, cur.HUDDebug, cur.Brightness = false, true, 10
	cur.HapticsGain["boost"] = 0.5
	cur.FireGroups["1"] = FireGroup{"beam", "beam"}
	cur.FireGroups["3"] = FireGroup{"beam", "missile"}
	cur.SpinUpMs["huge"] = 0
	cur.GyroOffGuiFocus = []int{6}
	cur.HUDColors["flash"] = "off"
	cur.Colors["hit"] = [3]int{1, 2, 3}
	cur.TriggerFX["hit"] = Trigger{"OFF", []int{}}
	cur.Rumble["boost"] = Rumble{0, 0, 1}
	reset := map[string]any{}
	for _, k := range Schema() {
		if k.Editable {
			reset[keyName(k)] = nil
		}
	}
	patch, err := json.Marshal(reset)
	if err != nil || len(reset) != 34 || len(patch) > control.MaxPatch {
		t.Fatalf("%d rows, %d bytes, %v", len(reset), len(patch), err)
	}
	next, changed, ps := MergePatch(cur, patch)
	want := Default()
	want.Version, want.Backend = cur.Version, cur.Backend
	if len(ps) > 0 || !reflect.DeepEqual(next, want) {
		t.Errorf("%s\nnext %+v\nwant %+v", problemsText(ps), next, want)
	}
	if !slices.Contains(changed, "hud_debug") || !slices.Contains(changed, "fire_groups.3") || slices.Contains(changed, "backend") {
		t.Errorf("changed %v", changed)
	}
}

// TestPatchOffTrigger: an OFF trigger as the window sends it, with
// "params": [], is taken where the default's params are null.
func TestPatchOffTrigger(t *testing.T) {
	cur := Default()
	if tr := cur.TriggerFX["ship_wep_empty_r"]; tr.Mode != "OFF" || tr.Params != nil {
		t.Fatalf("the default is %+v", tr)
	}
	next, _, ps := MergePatch(cur, []byte(`{"triggers": {"ship_wep_empty_r": {"mode": "OFF", "params": []}}}`))
	if tr := next.TriggerFX["ship_wep_empty_r"]; len(ps) > 0 || tr.Mode != "OFF" || len(tr.Params) != 0 {
		t.Errorf("%+v, %s", tr, problemsText(ps))
	}
}
