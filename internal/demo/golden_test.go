package demo

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/backend/backendtest"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/haptics"
)

var update = flag.Bool("update", false, "rewrite the transcripts in testdata/golden")

var base = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// TestGolden plays the demo on a scripted clock, to the end, cut short, and
// with rumble in place of native haptics, and compares all it asks of the
// backend with testdata/golden. The cut lands in the MENU TEST step, with
// the gyro off.
func TestGolden(t *testing.T) {
	for _, c := range []struct {
		name   string
		cutAt  time.Duration // 0: to the end
		rumble bool
	}{{"demo-full", 0, false}, {"demo-cut", 45 * time.Second, false}, {"demo-rumble", 0, true}} {
		t.Run(c.name, func(t *testing.T) {
			got := playDemo(t, c.cutAt, c.rumble)
			backendtest.Compare(t, filepath.Join("testdata", "golden", c.name+".txt"), got, *update)
		})
	}
}

func playDemo(t *testing.T, cutAt time.Duration, rumble bool) string {
	rec := &backendtest.Recorder{}
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(rec)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	}()

	now := base
	done := make(chan struct{})
	pad := &backendtest.Pad{Rec: rec, Open: true}
	synth := haptics.NewSynth()
	audio := &backendtest.Audio{Rec: rec, Up: true, Render: synth.Render}
	var out []string
	var prev []string
	var ear backendtest.Ear // hashes the whole demo's native haptics
	same := 0
	flush := func() {
		if same > 0 {
			out = append(out, fmt.Sprintf("  (same for %d more)", same))
		}
		same = 0
	}
	oldClock, oldSleep := clock, sleep
	defer func() { clock, sleep = oldClock, oldSleep }()
	clock = func() time.Time { return now }
	sleep = func(d time.Duration) {
		rec.Add("%s", ear.Listen(audio.Render, int(d/time.Millisecond)*48))
		lines := rec.Take()
		if prev != nil && slices.Equal(lines, prev) {
			same++
		} else {
			flush()
			out = append(out, fmt.Sprintf("@ %.3f sleep %v", now.Sub(base).Seconds(), d))
			for _, l := range lines {
				out = append(out, "  "+l)
			}
			prev = lines
		}
		now = now.Add(d)
		// the player's hands: R2 held through the firing steps
		pad.Hold(dualsense.State{})
		if el := now.Sub(base); el > 8*time.Second && el < 30*time.Second {
			pad.Hold(dualsense.State{Buttons: dualsense.R2, R2: 220})
		}
		if cutAt > 0 && now.Sub(base) >= cutAt {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	}

	cfg := config.Default()
	if rumble {
		cfg.HapticsMode = config.HapticsRumble
	}
	d := backendtest.NewDSX(t)
	d.Answering = true
	Run(&cfg, Output{Out: d, Outputs: outputs(&cfg), Pad: pad, Synth: synth, Audio: audio, Words: backend.DSXWords()}, done)
	flush()
	if s := ear.Sum(); s != "" {
		out = append(out, s)
	}
	out = append(out, "end")
	for _, l := range rec.Take() {
		out = append(out, "  "+l)
	}
	return strings.Join(out, "\n") + "\n"
}

func outputs(c *config.Config) dsx.Outputs {
	return dsx.Outputs{Triggers: c.Triggers, Lightbar: c.Lightbar, PlayerLEDs: c.PlayerLEDs, Mic: c.MicLED}
}
