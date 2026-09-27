package hud

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Captures from before midnight sort after those from after it by name:
// the oldest by the time they were written go first.
func TestPruneDebugKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 9, 27, 23, 50, 0, 0, time.UTC)
	for i := range debugFilesKept + 10 {
		at := start.Add(time.Duration(i) * time.Second)
		path := filepath.Join(dir, at.Format("150405.000")+fmt.Sprintf("_%d.png", i))
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	pruneDebug(dir)
	entries, _ := os.ReadDir(dir)
	if len(entries) != debugFilesKept {
		t.Fatalf("%d files kept", len(entries))
	}
	for i := range 10 {
		name := start.Add(time.Duration(i)*time.Second).Format("150405.000") + fmt.Sprintf("_%d.png", i)
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("the oldest file %s is kept", name)
		}
	}
}
