package elite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalTailer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Journal.2026-09-26T100000.01.log")
	write := func(p, s string) {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	write(path, `{"event":"Music","MusicTrack":"Supercruise"}`+"\n")
	j := NewJournalTailer(dir)
	t.Cleanup(j.Close) // Windows cannot remove an open file
	var names []string
	var live []bool
	handle := func(ev Event, l bool) { names, live = append(names, ev.Name()), append(live, l) }

	j.Poll(handle)
	write(path, `{"event":"ShieldState","ShieldsUp":false}`+"\n"+`{"event":"Hull`)
	j.Poll(handle)
	write(path, `Damage","Health":0.5}`+"\n")
	j.Poll(handle)
	if strings.Join(names, ",") != "Music,ShieldState,HullDamage" {
		t.Fatalf("events %v", names)
	}
	if live[0] || !live[1] || !live[2] {
		t.Fatalf("live %v: history must not be live, new lines must", live)
	}

	time.Sleep(10 * time.Millisecond)
	write(filepath.Join(dir, "Journal.2026-09-26T110000.01.log"), `{"event":"Fileheader"}`+"\n")
	j.lastCheck = time.Time{}
	j.Poll(handle)
	if names[len(names)-1] != "Fileheader" || !live[len(live)-1] {
		t.Fatalf("new journal not followed: %v %v", names, live)
	}
}

// TestJournalTailerClose: Close closes the file, so the folder can go,
// and Poll reads nothing after it.
func TestJournalTailerClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Journal.2026-09-26T100000.01.log")
	if err := os.WriteFile(path, []byte(`{"event":"Music"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := NewJournalTailer(dir)
	n := 0
	j.Poll(func(Event, bool) { n++ })
	if n != 1 || j.f == nil {
		t.Fatalf("%d events, file open %v", n, j.f != nil)
	}
	j.Close()
	j.Close()
	if err := os.Remove(path); err != nil {
		t.Fatalf("the journal is still open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Journal.2026-09-26T110000.01.log"), []byte(`{"event":"Fileheader"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	j.lastCheck = time.Time{}
	j.Poll(func(Event, bool) { n++ })
	if n != 1 || j.f != nil {
		t.Fatalf("Poll after Close: %d events, file open %v", n, j.f != nil)
	}
}

func TestStatusReaderKeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Status.json")
	r := NewStatusReader(dir)
	if err := os.WriteFile(path, []byte(`{"Flags":16777288,"FireGroup":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, changed := r.Poll()
	if !changed || !s.InShip() || !s.Flags.Has(HardpointsDeployed|ShieldsUp) || s.FireGroup != 1 {
		t.Fatalf("status %+v changed %v", s, changed)
	}
	if _, changed := r.Poll(); changed {
		t.Fatal("unchanged file reported as changed")
	}
	if err := os.WriteFile(path, []byte(`{"Flags":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, changed := r.Poll(); changed || !s.InShip() {
		t.Fatal("half-written file replaced the last good status")
	}
}

func TestLoadoutModules(t *testing.T) {
	ev := Event{"event": "Loadout", "Modules": []any{
		map[string]any{"Slot": "LargeHardpoint1", "Item": "hpt_multicannon_gimbal_large", "AmmoInClip": 77.0, "AmmoInHopper": 2100.0},
		map[string]any{"Slot": "MediumHardpoint1", "Item": "hpt_beamlaser_gimbal_medium"},
		map[string]any{"Slot": "TinyHardpoint1", "Item": "hpt_shieldbooster_size0_class5"},
		map[string]any{"Slot": "TinyHardpoint3", "Item": "hpt_heatsinklauncher_turret_tiny", "AmmoInClip": 1.0, "AmmoInHopper": 2.0},
		map[string]any{"Slot": "Slot02_Size6", "Item": "int_shieldcellbank_size6_class5", "AmmoInClip": 1.0, "AmmoInHopper": 4.0},
		map[string]any{"Slot": "Slot01_Size6", "Item": "int_shieldgenerator_size6_class3_fast"},
	}}
	var got []string
	for _, m := range LoadoutModules(ev) {
		got = append(got, fmt.Sprintf("%s/%s/%v/%s/%d", m.Name, m.Class, m.Utility, m.AmmoText, m.Size))
	}
	want := "MULTI-CANNON/multicannon/false/77/2100/3 BEAM LASER/beam/false//2 HEATSINK/heatsink/true/1/2/0 SHIELD CELL BANK/shieldcell/true/1/4/0"
	if strings.Join(got, " ") != want {
		t.Fatalf("modules\n%s\nwant\n%s", strings.Join(got, " "), want)
	}
}

func TestWeaponClass(t *testing.T) {
	for item, want := range map[string]string{
		"hpt_beamlaser_gimbal_medium":       "beam",
		"hpt_pulselaserburst_fixed_small":   "burst",
		"hpt_pulselaser_gimbal_large":       "pulse",
		"hpt_multicannon_gimbal_large":      "multicannon",
		"hpt_slugshot_fixed_medium":         "fragment",
		"hpt_railgun_fixed_medium":          "railgun",
		"hpt_plasmaaccelerator_fixed_large": "plasma",
		"hpt_basicmissilerack_fixed_small":  "missile",
		"hpt_cannon_gimbal_huge":            "cannon",
		"hpt_mininglaser_fixed_small":       "mining",
		"hpt_guardian_gausscannon_fixed":    "cannon",
		"hpt_guardian_shardcannon_fixed":    "cannon",
		"hpt_guardian_plasmalauncher_fixed": "plasma",
	} {
		if got := WeaponClass(item); got != want {
			t.Errorf("%s: %s, want %s", item, got, want)
		}
	}
}
