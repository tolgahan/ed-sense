package config

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/control"
)

// TestSchemaCoversConfig: every JSON field of Config has one row, in the
// file's order; each default is within its row's range; the closed maps'
// Keys are their default keys. hud_colors and fire_groups are open maps
// (defaults {} and "1", "2"), and backend's default "" is in its enum.
func TestSchemaCoversConfig(t *testing.T) {
	typ := reflect.TypeFor[Config]()
	keys := Schema()
	if len(keys) != typ.NumField() {
		t.Fatalf("%d rows for %d fields", len(keys), typ.NumField())
	}
	types := []string{typeBool, typeInt, typeFloat, typeEnum, typePath, typeRGB, typeHex, typeTrigger, typeRumble, typeFireGroup, typeIntSet}
	for i, k := range keys {
		f := typ.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if keyName(k) != name {
			t.Errorf("row %d is %s, the field is %s", i, k.Path, name)
			continue
		}
		if isMap(k) != (f.Type.Kind() == reflect.Map) {
			t.Errorf("%s: a map row %v for a %s", k.Path, isMap(k), f.Type)
		}
		if !slices.Contains(types, k.Type) {
			t.Errorf("%s: type %q", k.Path, k.Type)
		}
		if !slices.Contains(control.Pages, k.Page) {
			t.Errorf("%s: page %q", k.Path, k.Page)
		}
		if k.Default == nil {
			t.Errorf("%s: no default", k.Path)
		}
		var c checker
		if !isMap(k) {
			c.value(k, k.Path, k.Default)
		} else {
			entries, ok := k.Default.(map[string]any)
			if !ok {
				t.Errorf("%s: default %T", k.Path, k.Default)
				continue
			}
			for _, e := range sortedKeys(entries) {
				path := name + "." + e
				// an OFF trigger's params are null in JSON, which a patch
				// may not send: triggers are checked whole below
				if c.entryName(k, path, e) && k.Type != typeTrigger {
					c.entry(k, path, e, entries[e])
				}
			}
		}
		if len(c.problems) > 0 {
			t.Errorf("%s: the default is out of range: %+v", k.Path, c.problems)
		}
	}

	d := Default()
	index := schemaIndex()
	for name, want := range map[string][]string{
		"colors": sortedKeys(d.Colors), "triggers": sortedKeys(d.TriggerFX), "haptics_gain": sortedKeys(d.HapticsGain),
		"rumble": sortedKeys(d.Rumble), "spin_up_ms": sortedKeys(d.SpinUpMs),
	} {
		if k := index[name]; len(want) == 0 || !slices.Equal(k.Keys, want) {
			t.Errorf("%s: keys %v, the defaults have %v", name, k.Keys, want)
		}
	}
	if k := index["hud_colors"]; !slices.Equal(k.Keys, []string{"shield", "heat", "flame", "flash", "hull"}) {
		t.Errorf("hud_colors keys %v", k.Keys)
	}
	if k := index["fire_groups"]; len(k.Keys) != 0 || k.Min != 1 || k.Max != 99 || len(k.Enum) != 12 {
		t.Errorf("fire_groups: keys %v, %v-%v, %v", k.Keys, k.Min, k.Max, k.Enum)
	}
	for name, tr := range d.TriggerFX {
		var c checker
		if c.trigger("triggers."+name, tr); len(c.problems) > 0 {
			t.Errorf("default trigger %s: %+v", name, c.problems)
		}
	}
	if k := index["backend"]; !slices.Contains(k.Enum, d.Backend) {
		t.Errorf("backend's default %q is not in %v", d.Backend, k.Enum)
	}
}

// TestSchemaRestartKeys: the rows with a restart are RestartKeys' keys.
func TestSchemaRestartKeys(t *testing.T) {
	a, b := Default(), Default()
	b.Backend, b.DS4WindowsPort, b.DSXPort, b.JournalDir, b.BindingsDir, b.PollMs = BackendDSX, 1, 2, "j", "b", 30
	want := RestartKeys(&a, &b)
	var got []string
	for _, k := range Schema() {
		switch k.Restart {
		case "":
		case RestartSession, RestartBackend:
			got = append(got, k.Path)
		default:
			t.Errorf("%s: restart %q", k.Path, k.Restart)
		}
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("restart rows %v, RestartKeys %v", got, want)
	}
}

// TestSchemaEditable: a patch sets everything but the version, the backend
// (backend.choose), the folders (Notepad) and hud_debug (screen captures).
func TestSchemaEditable(t *testing.T) {
	var fixed []string
	for _, k := range Schema() {
		if !k.Editable {
			fixed = append(fixed, k.Path)
		}
	}
	if want := []string{"config_version", "backend", "journal_dir", "bindings_dir", "hud_debug"}; !slices.Equal(fixed, want) {
		t.Errorf("not editable: %v, want %v", fixed, want)
	}
}

// TestSchemaJSON: settings.schema sends the rows as JSON, defaults as
// numbers, and each call gets its own copy.
func TestSchemaJSON(t *testing.T) {
	keys := Schema()
	b, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	var back []map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	byPath := map[string]map[string]any{}
	for _, k := range back {
		byPath[k["path"].(string)] = k
	}
	if p := byPath["poll_ms"]; p["default"] != 25.0 || p["min"] != 20.0 || p["max"] != 100.0 || p["restart"] != "session" || p["editable"] != true {
		t.Errorf("poll_ms: %v", p)
	}
	if c := byPath["colors.*"]["default"].(map[string]any)["hit"]; !reflect.DeepEqual(c, []any{255.0, 30.0, 30.0}) {
		t.Errorf("colors.hit default %v", c)
	}
	keys[0].Path = "changed"
	keys[1].Enum[0] = "changed"
	if again := Schema(); again[0].Path != "config_version" || again[1].Enum[0] != "" {
		t.Error("Schema hands out shared rows")
	}
}
