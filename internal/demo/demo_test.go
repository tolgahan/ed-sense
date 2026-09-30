package demo

import (
	"log"
	"slices"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/backend/backendtest"
	"github.com/tolgahan/ed-sense/internal/config"
)

// TestHapticsLine: the demo says which haptics it plays. Where rumble
// would mute native haptics (DS4Windows), it says why there are none, or
// that they are off.
func TestHapticsLine(t *testing.T) {
	rec := &backendtest.Recorder{}
	oldOut, oldFlags, oldSleep := log.Writer(), log.Flags(), sleep
	log.SetOutput(rec)
	log.SetFlags(0)
	sleep = func(time.Duration) {}
	defer func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
		sleep = oldSleep
	}()
	d := backendtest.NewDSX(t)
	d.Answering = true
	dsxWords, ds4w := backend.DSXWords(), backend.DS4WindowsWords()
	for _, c := range []struct {
		name    string
		words   backend.Words
		haptics bool
		mode    string
		audio   bool
		want    string
	}{
		{"DSX native", dsxWords, true, config.HapticsAuto, true, "Haptics: native (virtual DualSense audio)"},
		{"DSX no audio", dsxWords, true, config.HapticsAuto, false, "Haptics: rumble fallback"},
		{"DS4Windows native", ds4w, true, config.HapticsAuto, true, "Haptics: native (virtual DualSense audio)"},
		{"DS4Windows no audio", ds4w, true, config.HapticsAuto, false, ds4w.DemoNoFallback},
		{"DS4Windows rumble", ds4w, true, config.HapticsRumble, true, "Haptics: rumble fallback"},
		{"DS4Windows off", ds4w, false, config.HapticsAuto, true, "Haptics: off"},
	} {
		cfg := config.Default()
		cfg.Haptics, cfg.HapticsMode = c.haptics, c.mode
		pad := &backendtest.Pad{Rec: rec, Open: true}
		audio := &backendtest.Audio{Rec: rec, Up: c.audio}
		prepare(&cfg, Output{Out: d, Pad: pad, Audio: audio, Words: c.words})
		if lines := rec.Take(); !slices.Contains(lines, "log "+c.want) {
			t.Errorf("%s: %q, want %q", c.name, lines, c.want)
		}
	}
}
