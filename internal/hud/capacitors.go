package hud

import (
	"fmt"
	"math"
	"sort"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// Right of the ship hologram are the power distributor's capacitors: SYS,
// ENG and WEP, each a column of about ten segments lit from the bottom up to
// the charge, the rest dim. The lit segments blur into one bright block; the
// empty part above is a darker, redder orange. All three have the same height.

// capacitorBox: where the bars are, relative to the shield %, in its glyph heights.
var capacitorBox = box{6, -9, 26, 7}

type capBar struct {
	x0, x1 int
	bottom int
	litTop int // top of the lit part
	top    int // top of the bar, lit or dim
}

func (b capBar) mid() float64 { return float64(b.x0+b.x1) / 2 }

// readCapacitors reads the three bars next to the shield % at (x, y).
func readCapacitors(im *vision.Image, x, y, gh float64, pal Palette) (charge [3]float64, ok bool) {
	r := capacitorBox.around(x, y, gh, 0, 0).Clip(im.W, im.H)
	W, H := r.W(), r.H()
	if W < 8 || H < 8 {
		return charge, false
	}
	classes := orangeClasses(im, r, pal)
	anyOrange := make([]bool, W*H)
	bright := make([]bool, W*H)
	for i, c := range classes {
		anyOrange[i], bright[i] = c != 0, c == 2
	}
	// how far up the (dim) bar goes above row y0, in columns x0..x1
	extent := func(x0, x1, y0 int) int {
		top, miss := y0, 0
		for y := y0 - 1; y >= 0 && miss <= int(0.3*gh); y-- {
			n := 0
			for x := x0; x < x1; x++ {
				if anyOrange[y*W+x] {
					n++
				}
			}
			if float64(n) >= 0.5*float64(x1-x0) {
				top, miss = y, 0
			} else {
				miss++
			}
		}
		return top
	}
	_, comps := vision.Components(bright, W, H)
	var bars []capBar
	for _, c := range comps {
		w, h := float64(c.W)/gh, float64(c.H)/gh
		if w < 1.0 || w > 2.6 || h < 0.25 || h > 6 || float64(c.Area) < 0.5*float64(c.W*c.H) {
			continue
		}
		inner0, inner1 := c.X+c.W/5, c.X+c.W-c.W/5
		bars = append(bars, capBar{c.X, c.X + c.W, c.Y + c.H, c.Y, extent(inner0, inner1, c.Y)})
	}
	bars = onePerColumn(bars)
	sort.Slice(bars, func(i, j int) bool { return bars[i].x0 < bars[j].x0 })
	three, found := evenlySpaced(bars, gh)
	if !found {
		return charge, false
	}
	full := 0
	for _, b := range three {
		full = max(full, b.bottom-b.top)
	}
	if float64(full) < 2*gh {
		return charge, false
	}
	for i, b := range three {
		charge[i] = math.Min(1, float64(b.bottom-b.litTop)/float64(full))
	}
	return charge, true
}

// onePerColumn: the pip squares under a bar are a bright block too; the
// highest block in a column is the bar's lit part, or the pips of an empty bar.
func onePerColumn(bars []capBar) []capBar {
	sort.Slice(bars, func(i, j int) bool { return bars[i].litTop < bars[j].litTop })
	var out []capBar
	for _, b := range bars {
		dup := false
		for _, u := range out {
			overlap := min(b.x1, u.x1) - max(b.x0, u.x0)
			if float64(overlap) > 0.5*float64(min(b.x1-b.x0, u.x1-u.x0)) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, b)
		}
	}
	return out
}

// evenlySpaced picks the three bars (sorted left to right) that are most
// evenly spaced, 2-4.5 glyph heights apart.
func evenlySpaced(bars []capBar, gh float64) ([3]capBar, bool) {
	var best [3]capBar
	bestErr, found := math.Inf(1), false
	for a := range bars {
		for b := a + 1; b < len(bars); b++ {
			for c := b + 1; c < len(bars); c++ {
				d1, d2 := (bars[b].mid()-bars[a].mid())/gh, (bars[c].mid()-bars[b].mid())/gh
				if d1 < 2 || d1 > 4.5 || d2 < 2 || d2 > 4.5 {
					continue
				}
				if e := math.Abs(d1 - d2); e < 1 && e < bestErr {
					best, bestErr, found = [3]capBar{bars[a], bars[b], bars[c]}, e, true
				}
			}
		}
	}
	return best, found
}

// orangeClasses: 1 = the HUD's main colour, lit or dim (dim is darker and
// redder), 2 = lit.
func orangeClasses(im *vision.Image, r vision.Rect, pal Palette) []uint8 {
	hue, _, _ := pal.Main.HSV()
	return vision.Classify(im, r, fmt.Sprintf("orange-%.0f", hue), func(red, g, b float32) uint8 {
		h, s, v := vision.HSV(red, g, b)
		switch {
		case s >= 0.55 && v >= 0.45 && vision.HueDistance(h, hue) <= 20:
			return 2
		case s >= 0.55 && v >= 0.12 && vision.HueDistance(h, hue) <= 28:
			return 1
		}
		return 0
	})
}
