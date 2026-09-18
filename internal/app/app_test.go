package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/lights"
)

// TestReplayJournals plays real journals through the game and the lights.
// Set EDSENSE_JOURNALS to a folder of Journal.*.log files.
func TestReplayJournals(t *testing.T) {
	dir := os.Getenv("EDSENSE_JOURNALS")
	if dir == "" {
		t.Skip("EDSENSE_JOURNALS not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "Journal.*.log"))
	cfg := config.Default()
	events := 0
	for _, name := range files {
		g := game.New()
		r := lights.New(&cfg)
		base := time.Now()
		g.OnStatus(elite.Status{Flags: elite.InMainShip | elite.ShieldsUp}, base)
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		lines := bufio.NewScanner(f)
		lines.Buffer(make([]byte, 1<<20), 1<<24)
		for lines.Scan() {
			var ev elite.Event
			if json.Unmarshal(lines.Bytes(), &ev) != nil {
				continue
			}
			events++
			now := base.Add(time.Duration(events) * 100 * time.Millisecond)
			g.OnEvent(ev, true, now)
			r.Frame(g, now)
		}
		f.Close()
		t.Logf("%s: hull %.2f, music %q, closed %v", filepath.Base(name), g.Hull, g.Music, g.Closed)
	}
	t.Logf("%d events from %d files", events, len(files))
}
