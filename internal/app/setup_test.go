package app

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestSetupWiring: the session builds the DSX profile work with its backups
// in the data folder, telling the player through the app.
func TestSetupWiring(t *testing.T) {
	p := newPlayer(t, nil)
	if want := filepath.Join(p.dir, "dsx_profile_backups"); p.backupDir != want {
		t.Fatalf("backups go to %q, want %q", p.backupDir, want)
	}
	p.notify("hello")
	if got := p.rec.Take(); !slices.Contains(got, `notify "hello"`) {
		t.Errorf("the message did not reach the player: %q", got)
	}
}
