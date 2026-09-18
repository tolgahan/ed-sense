package elite

import (
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
