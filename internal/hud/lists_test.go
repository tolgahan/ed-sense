package hud

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// The width model against the mean widths measured on 4K captures.
func TestTextWidth(t *testing.T) {
	for _, c := range []struct {
		name   string
		weapon bool
		want   float64
	}{{"MULTI-CANNON", true, 12.1}, {"BEAM LASER", true, 10.0}, {"HEATSINK", false, 6.75}, {"SECONDARY A", false, 9.8}} {
		w := textWidth(c.name)
		if c.weapon {
			w += iconWidth
		}
		if d := w/c.want - 1; d < -0.06 || d > 0.06 {
			t.Errorf("%s: %.2f, measured %.2f", c.name, w, c.want)
		}
	}
}

func listCaptures(t *testing.T) []string {
	files, _ := filepath.Glob(filepath.Join(dataDir(t, "HUD_WPN"), "*_weapons*.png"))
	if len(files) == 0 {
		t.Skip("no captures")
	}
	sort.Strings(files)
	return files
}

// The test pilot's lists: left (SECONDARY) 3 multi-cannons and 2 heat sinks,
// right (PRIMARY) 2 beam lasers. Three captures show a multi-cannon
// RELOADING, one DEPLOYING, some OUT OF RANGE. They are also read scaled
// down to 1440p and 1080p.
func TestListsReal(t *testing.T) {
	files := listCaptures(t)
	mods := kraitLoadout()
	reloading := map[string]bool{"214002.415": true, "214210.714": true, "214213.714": true}
	for _, scale := range []float64{1, 2.0 / 3, 0.5} {
		var nLeft, nRight, multiCannons, heatSinks, beams, wrong, reloads, wrongReloads int
		for _, f := range files {
			base := filepath.Base(f)
			right := strings.Contains(base, "weaponsR")
			im := loadImage(t, f)
			screenH := 2160
			if scale != 1 {
				im = downscale(im, scale)
				screenH = int(2160 * scale)
			}
			names := map[string]int{}
			reload := false
			for _, e := range readList(im, vision.Rect{X1: im.W, Y1: im.H}, screenH, DefaultPalette, right).Entries {
				i, _ := MatchEntry(e, mods)
				if i < 0 {
					continue
				}
				names[mods[i].Name]++
				if mods[i].Name == "MULTI-CANNON" && e.Sub == StatusLine && e.Clip < 0.1 {
					reload = true
				}
			}
			if right {
				nRight++
				if names["BEAM LASER"] > 0 {
					beams++
				}
				if len(names) > 1 || names["BEAM LASER"] > 2 {
					wrong++
				}
			} else {
				nLeft++
				if names["MULTI-CANNON"] > 0 {
					multiCannons++
				}
				if names["HEATSINK"] > 0 {
					heatSinks++
				}
				if names["BEAM LASER"] > 0 || names["SHIELD CELL BANK"] > 0 || names["MULTI-CANNON"] > 3 {
					wrong++
				}
			}
			switch {
			case reload && reloading[base[:10]]:
				reloads++
			case reload:
				wrongReloads++
				t.Logf("scale %.2f %s: reloading?", scale, base)
			}
		}
		t.Logf("scale %.2f: left %d (multi-cannons in %d, heat sinks in %d), right %d (beams in %d), odd reads %d, reloads %d/3 (+%d wrong)",
			scale, nLeft, multiCannons, heatSinks, nRight, beams, wrong, reloads, wrongReloads)
		// a few captures have the multi-cannons out of range or out of view
		if multiCannons < nLeft*80/100 || heatSinks < nLeft*85/100 || beams < nRight || wrongReloads > 0 || reloads < 2 || wrong > nLeft/10 {
			t.Errorf("scale %.2f: too many misses", scale)
		}
	}
}

// The watcher follows both lists on a composed 4K screen and learns the fire
// group; a RELOADING capture gives a reload.
func TestWatcherLists(t *testing.T) {
	files := listCaptures(t)
	pick := func(prefix, side string) *vision.Image {
		for _, f := range files {
			if b := filepath.Base(f); strings.HasPrefix(b, prefix) && strings.Contains(b, side) {
				return loadImage(t, f)
			}
		}
		t.Skipf("capture %s %s missing", prefix, side)
		return nil
	}
	compose := func(left, right *vision.Image) *vision.Image {
		im := vision.NewImage(3840, 2160)
		paste := func(src *vision.Image, x0, y0 int) {
			for y := range src.H {
				copy(im.Pix[(y0+y)*im.Stride+x0*4:], src.Pix[y*src.Stride:y*src.Stride+src.W*4])
			}
		}
		paste(left, 460, 648) // where the captures were taken
		paste(right, 2112, 648)
		return im
	}
	right := pick("214201.714", "weaponsR")
	normal := compose(pick("214207.714", "weaponsL"), right)
	busy := compose(pick("214210.714", "weaponsL"), right)
	done := compose(pick("214216.814", "weaponsL"), right)

	screen := &frameGrabber{normal}
	w := NewWatcher(screen)
	w.SetActive(true, true, "")
	w.SetFast(true)
	w.SetLoadout(kraitLoadout())
	now := testStart
	key := FireKey(0, true)
	w.SetFireGroups(true, key, true, [2]bool{}, now.Add(-10*time.Second))
	var events []string
	run := func(d time.Duration, held [2]bool) {
		for end := now.Add(d); now.Before(end); now = now.Add(100 * time.Millisecond) {
			w.SetFireGroups(true, key, true, held, now)
			w.tick(now)
			for _, e := range w.Take().Events {
				if e.Kind == ReloadStart || e.Kind == ReloadDone {
					events = append(events, fmt.Sprintf("%s/%d", e.Kind, e.Side))
				}
			}
		}
	}
	start := time.Now()
	run(5*time.Second, [2]bool{})
	st := w.Take()
	left, rightList := st.Lists[Secondary], st.Lists[Primary]
	if !left.Known || !rightList.Known {
		t.Fatalf("lists not learned in 5 s: %+v / %+v", left, rightList)
	}
	t.Logf("L2: %s %v, R2: %s %v", left, left.Classes, rightList, rightList.Classes)
	if left.Classes[0] != "multicannon" || rightList.Classes[0] != "beam" {
		t.Fatal("wrong weapons")
	}
	if w.lists[Secondary].win == nil || w.lists[Primary].win == nil {
		t.Fatal("lists not tracked")
	}
	// firing L2: the left list is read 4 times a second; a reload comes and goes
	screen.cur = busy
	run(time.Second, [2]bool{true, false})
	screen.cur = done
	run(time.Second, [2]bool{true, false})
	if strings.Join(events, " ") != "reload_start/1 reload_done/1" {
		t.Fatalf("reload events: %v", events)
	}
	t.Logf("%d reads in %.1f s of play, %.1f ms each", w.stats.reads, now.Sub(testStart).Seconds(), float64(time.Since(start).Microseconds())/1000/float64(w.stats.reads))
}

// TestWatcherSequence plays a 10 fps recording through the watcher
// (tracking windows, filtering, hit events) and logs what it saw.
func TestWatcherSequence(t *testing.T) {
	dir := dataDir(t, "HUD_SEQ")
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".png") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	screen := &frameGrabber{}
	w := NewWatcher(screen)
	w.SetActive(true, true, "")
	w.SetFast(true)
	now := testStart
	counts := map[EventKind]int{}
	for i, n := range names {
		screen.cur = loadImage(t, filepath.Join(dir, n))
		w.lastFull = time.Time{} // frames are 100 ms apart: every one may rescan
		w.tick(now)
		st := w.Take()
		var ev []string
		for _, e := range st.Events {
			counts[e.Kind]++
			ev = append(ev, fmt.Sprintf("%s(%.2f,%+.1f)", e.Kind, e.Strength, e.Pan))
		}
		t.Logf("t=%4.1fs shield %3d%% (%v) heat %3d%% splash %4.1f tracking %v hull %d target %d/%d blue %v caps %.1f %s",
			float64(i)/10, st.Shield.Value, st.Shield.OK, st.Heat.Value, st.Splash, w.shieldWin != nil,
			st.Hull.Value, st.TargetShield.Value, st.TargetHull.Value, st.BlueZone.Value, st.Capacitors.Value, strings.Join(ev, " "))
		now = now.Add(100 * time.Millisecond)
	}
	t.Logf("%d frames, events %v", len(names), counts)
}
