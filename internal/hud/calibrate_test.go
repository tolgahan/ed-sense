package hud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// TestCalibrate finds the HUD colours from the shield % / hull % layout
// (golden_calib.json in HUD_FRAMES), then reads with them.
func TestCalibrate(t *testing.T) {
	dir := dataDir(t, "HUD_FRAMES")
	raw, err := os.ReadFile(filepath.Join(dir, "golden_calib.json"))
	if err != nil {
		t.Skip(err)
	}
	var golden map[string]*struct {
		Shield     vision.Color `json:"shield"`
		Main       vision.Color `json:"main"`
		ShieldText string       `json:"shield_text"`
		HullText   string       `json:"hull_text"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	for n, want := range golden {
		im := loadImage(t, filepath.Join(dir, n))
		c := calibrate(im, shieldArea(im.W, im.H), im.H)
		if want == nil {
			if c.OK {
				t.Logf("%s: found %s/%s where the reference found nothing", n, c.ShieldText, c.HullText)
			}
			continue
		}
		if !c.OK {
			t.Errorf("%s: nothing found, reference %s/%s", n, want.ShieldText, want.HullText)
			continue
		}
		if c.ShieldText != want.ShieldText || c.HullText != want.HullText {
			t.Errorf("%s: read %s/%s, reference %s/%s", n, c.ShieldText, c.HullText, want.ShieldText, want.HullText)
		}
		// the reference takes the colour group's colour, calibration the text's own
		if d := vision.ColorDistance(c.Shield, want.Shield); d > 0.3 {
			t.Errorf("%s: shield colour %v, reference %v (%.2f)", n, c.Shield, want.Shield, d)
		}
		if d := vision.ColorDistance(c.Main, want.Main); d > 0.3 {
			t.Errorf("%s: main colour %v, reference %v (%.2f)", n, c.Main, want.Main, d)
		}
		pal := DefaultPalette
		pal.Shield = c.Shield
		pal.Heat, _ = calibrateHeat(im, heatArea(im.W, im.H), im.H, c.Main, pal)
		r := Read(im, pal)
		t.Logf("%s: shield %s main %s heat %s: reads shield %q heat %q", n, c.Shield.Hex(), c.Main.Hex(), pal.Heat.Hex(), r.Shield.Text, r.Heat.Text)
		if r.Shield.Text != want.ShieldText {
			t.Errorf("%s: with the colours found, shield reads %q, want %q", n, r.Shield.Text, want.ShieldText)
		}
	}
}

// runWatcher ticks a watcher at 10 per second.
func runWatcher(w *Watcher, from time.Time, ticks int, stop func() bool) time.Time {
	now := from
	for range ticks {
		if stop != nil && stop() {
			break
		}
		w.tick(now)
		now = now.Add(100 * time.Millisecond)
	}
	return now
}

// A watcher with the wrong colours finds the right ones after 30 s of not
// reading the shield %.
func TestWatcherLearnsColours(t *testing.T) {
	dir := dataDir(t, "HUD_FRAMES")
	im := loadImage(t, filepath.Join(dir, "icy_0121.png"))
	w := NewWatcher(&frameGrabber{im})
	var learned *Palette
	w.SetPalette(DefaultPalette, true, func(p Palette) { learned = &p })
	w.SetActive(true, true, "")
	end := runWatcher(w, testStart, 1200, func() bool { return learned != nil })
	if learned == nil {
		t.Fatal("colours not learned in 120 s")
	}
	t.Logf("learned after %.0f s: shield %s heat %s", end.Sub(testStart).Seconds(), learned.Shield.Hex(), learned.Heat.Hex())
	if r := Read(im, *learned); r.Shield.Text != "41%" || r.Heat.Text != "32%" {
		t.Fatalf("with the learned colours: shield %q heat %q", r.Shield.Text, r.Heat.Text)
	}
}

// The standard HUD with the standard colours: checked, nothing learned.
func TestWatcherChecksColours(t *testing.T) {
	dir := dataDir(t, "HUD_FRAMES")
	w := NewWatcher(&frameGrabber{loadImage(t, filepath.Join(dir, "f_0004.png"))})
	changed := false
	w.SetPalette(DefaultPalette, true, func(Palette) { changed = true })
	w.SetActive(true, true, "")
	runWatcher(w, testStart, 600, nil)
	if changed || !w.learner.verified {
		t.Fatalf("standard colours: changed %v, verified %v", changed, w.learner.verified)
	}
}

// Colours set by hand are never replaced.
func TestWatcherKeepsManualColours(t *testing.T) {
	dir := dataDir(t, "HUD_FRAMES")
	w := NewWatcher(&frameGrabber{loadImage(t, filepath.Join(dir, "icy_0121.png"))})
	called := false
	w.SetPalette(DefaultPalette, false, func(Palette) { called = true })
	w.SetActive(true, true, "")
	runWatcher(w, testStart, 1200, nil)
	if called {
		t.Fatal("learned although the colours were set by hand")
	}
}
