package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/lights"
)

func TestMotionPolicy(t *testing.T) {
	for _, c := range []struct {
		name                                     string
		running, online, paused, gyroAim, inMenu bool
		off                                      bool
	}{
		{"flying with gyro aim", true, true, false, true, false, false},
		{"in a menu", true, true, false, true, true, true},
		{"gyro aim off", true, true, false, false, false, true},
		{"paused", true, true, true, false, true, false},
		{"Elite closed", false, true, false, false, true, false},
		{"DSX not answering", true, false, false, false, true, false},
	} {
		if got := motionOff(c.running, c.online, c.paused, c.gyroAim, c.inMenu); got != c.off {
			t.Errorf("%s: motion off %v, want %v", c.name, got, c.off)
		}
	}
}

// TestReplayJournals plays real journals through the game, the haptics and
// the lights. Set EDSENSE_JOURNALS to a folder of Journal.*.log files.
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
		h := haptics.New(&cfg)
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
			h.OnEvent(ev, g, now)
			r.Frame(g, now)
			if left, right := h.Tick(now, g, dualsense.State{OK: true, R2: 200}); left < 0 || left > 1 || right < 0 || right > 1 {
				t.Fatalf("motor levels out of range: %v %v", left, right)
			}
		}
		f.Close()
		t.Logf("%s: hull %.2f, music %q, closed %v", filepath.Base(name), g.Hull, g.Music, g.Closed)
	}
	t.Logf("%d events from %d files", events, len(files))
}
