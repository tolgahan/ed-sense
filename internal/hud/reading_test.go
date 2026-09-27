package hud

import (
	"path/filepath"
	"testing"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// readFrames reads the labelled recording, recoloured by m (nil: as
// recorded), and returns the shield and heat hit rates.
func readFrames(t *testing.T, dir string, m *[3][3]float64, pal Palette) (shield, heat float64) {
	shieldLabels, shieldNames := loadLabels(t, filepath.Join(dir, "labels_shield.json"))
	heatLabels, heatNames := loadLabels(t, filepath.Join(dir, "labels_heat.json"))
	cache := map[string]Reading{}
	read := func(name string) Reading {
		if r, ok := cache[name]; ok {
			return r
		}
		im := loadImage(t, filepath.Join(dir, name))
		if m != nil {
			im = recolour(im, *m)
		}
		cache[name] = Read(im, pal)
		return cache[name]
	}
	var s, h int
	for _, n := range shieldNames {
		if r := read(n); sameValue(r.Shield, shieldLabels[n]) {
			s++
		} else {
			t.Logf("%s shield %q, want %s", n, r.Shield.Text, shieldLabels[n])
		}
	}
	for _, n := range heatNames {
		if r := read(n); sameValue(r.Heat, heatLabels[n]) {
			h++
		} else {
			t.Logf("%s heat %q, want %s", n, r.Heat.Text, heatLabels[n])
		}
	}
	return float64(s) / float64(len(shieldNames)), float64(h) / float64(len(heatNames))
}

func TestRecordingAccuracy(t *testing.T) {
	s, h := readFrames(t, dataDir(t, "HUD_FRAMES"), nil, DefaultPalette)
	t.Logf("shield %.0f%%, heat %.0f%%", s*100, h*100)
	if s < 0.85 || h < 0.75 {
		t.Error("accuracy too low")
	}
}

func Test4KCaptureAccuracy(t *testing.T) {
	dir := dataDir(t, "HUD_CROPS")
	for _, kind := range []string{"shield", "heat"} {
		labels, names := loadLabels(t, filepath.Join(dir, "truth_"+kind+".json"))
		ok := 0
		for _, n := range names {
			im := loadImage(t, filepath.Join(dir, n))
			all := vision.Rect{X1: im.W, Y1: im.H}
			var got Number
			if kind == "shield" {
				got, _, _ = readShield(im, all, 2160, DefaultPalette)
			} else {
				got = readHeat(im, all, 2160, DefaultPalette)
			}
			if sameValue(got, labels[n]) {
				ok++
			} else {
				t.Logf("%s %s %q, want %s", n[:10], kind, got.Text, labels[n])
			}
		}
		acc := float64(ok) / float64(len(names))
		t.Logf("%s %d/%d (%.0f%%)", kind, ok, len(names), acc*100)
		if kind == "shield" && acc < 0.95 || kind == "heat" && acc < 0.85 {
			t.Errorf("%s accuracy too low", kind)
		}
	}
}

// TestColourMatrixFrames reads the recording recoloured with colour
// matrices, using the palette predicted from each. The whole lower half is
// recoloured (in the game only the HUD is), which makes it harder.
func TestColourMatrixFrames(t *testing.T) {
	dir := dataDir(t, "HUD_FRAMES")
	for _, c := range []struct {
		name         string
		m            [3][3]float64
		shield, heat float64 // minimum accuracy
	}{
		{"icy", icyMatrix, 0.8, 0.4},
		{"green", [3][3]float64{{0, 0.38, 0}, {0, 0, 0}, {1, 0.64, 1}}, 0.8, 0.4},
		{"purple", [3][3]float64{{0.8, 0, 1}, {0, 0.3, 0.6}, {0, 1, 0}}, 0.7, 0.4},
	} {
		s, h := readFrames(t, dir, &c.m, DefaultPalette.underMatrix(c.m).withFlashCheck())
		t.Logf("%s: shield %.0f%%, heat %.0f%%", c.name, s*100, h*100)
		if s < c.shield || h < c.heat {
			t.Errorf("%s: accuracy too low", c.name)
		}
	}
}

// The blue zone reader on a drawn throttle arc: the marker is pale blue in
// the zone and orange outside it, as in the game.
func TestBlueZoneSynthetic(t *testing.T) {
	const gh = 16.0
	draw := func(markerY int, pale, zone bool) *vision.Image {
		im := vision.NewImage(700, 500)
		set := func(x, y int, r, g, b byte) {
			i := y*im.Stride + x*4
			im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = b, g, r, 255
		}
		colX := int(100 + 18*gh) // the heat % is at (100, 100)
		for seg := 0; zone && seg < 8; seg++ {
			y0 := 150 + seg*14
			for y := y0; y < y0+10; y++ {
				for x := colX; x < colX+12; x++ {
					set(x, y, 170, 205, 205)
				}
			}
		}
		for x := colX; x < colX+70; x++ { // the marker, broken by chevrons
			if (x/9)%3 == 2 {
				continue
			}
			r, g, b := byte(165), byte(200), byte(205)
			if !pale {
				r, g, b = 230, 150, 20
			}
			set(x, markerY, r, g, b)
			set(x, markerY+1, r, g, b)
		}
		return im
	}
	for _, c := range []struct {
		y                int
		pale, zone       bool
		wantSeen, wantIn bool
	}{
		{200, true, true, true, true},
		{158, true, true, true, true},
		{110, false, true, true, false},
		{300, false, true, true, false},
		{200, false, false, false, false},
	} {
		seen, in := readBlueZone(draw(c.y, c.pale, c.zone), 100, 100, gh)
		if seen != c.wantSeen || in != c.wantIn {
			t.Errorf("marker at %d, pale %v, zone %v: seen %v, in %v", c.y, c.pale, c.zone, seen, in)
		}
	}
}
