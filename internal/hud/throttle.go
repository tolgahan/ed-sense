package hud

import "github.com/tolgahan/ed-sense/internal/hud/vision"

// Right of the radar is the throttle arc: filled chevrons for the speed, a
// column of pale blue segments for the blue zone (the best turn rate) and a
// thin line ending in a small diamond for the throttle setting. The line is
// pale blue like the zone while the throttle is in it, and orange when not.

// blueZoneBox: where the arc is, relative to the heat %, in its glyph heights.
var blueZoneBox = box{12, -3, 26, 16}

// readBlueZone looks for the blue zone next to the heat % at (x, y): whether
// its column is in view, and whether the throttle marker is pale beside it.
// The marker is thin, slanted and broken where it crosses the chevrons, so it
// is taken as a cloud of pixels.
func readBlueZone(im *vision.Image, x, y, gh float64) (seen, in bool) {
	r := blueZoneBox.around(x, y, gh, 0, 0).Clip(im.W, im.H)
	if r.W() < 8 || r.H() < 8 {
		return false, false
	}
	pale := paleBlue(im, r)
	W, H := r.W(), r.H()
	perColumn := make([]float64, W)
	for yy := range H {
		for xx := range W {
			if pale[yy*W+xx] {
				perColumn[xx]++
			}
		}
	}
	// the column: the band 1.5 gh wide holding the most pale pixels, where the
	// arc is (the ship panel's rings are further right)
	band := max(1, int(1.5*gh))
	bestX, best := 0, 0.0
	for x0 := int(0.15 * float64(W)); x0+band <= int(0.65*float64(W)); x0++ {
		var n float64
		for k := x0; k < x0+band; k++ {
			n += perColumn[k]
		}
		if n > best {
			bestX, best = x0, n
		}
	}
	if best < 0.3*gh*gh {
		return false, false
	}
	// the marker, if pale: pixels just right of the column
	right := bestX + band
	m0, m1 := right+int(0.3*gh), min(W, right+int(2.5*gh))
	var ys []float64
	for yy := range H {
		for xx := m0; xx < m1; xx++ {
			if pale[yy*W+xx] {
				ys = append(ys, float64(yy))
			}
		}
	}
	return true, float64(len(ys)) >= 0.1*gh*gh && vision.Percentile(ys, 90)-vision.Percentile(ys, 10) <= 1.5*gh
}

// paleBlue: the zone's pale blue, between white and the saturated blue of
// radar contacts.
func paleBlue(im *vision.Image, r vision.Rect) []bool {
	classes := vision.Classify(im, r, "pale-blue", func(red, g, b float32) uint8 {
		if h, s, v := vision.HSV(red, g, b); v > 0.5 && s > 0.08 && s < 0.55 && h > 160 && h < 215 {
			return 1
		}
		return 0
	})
	mask := make([]bool, len(classes))
	for i, c := range classes {
		mask[i] = c != 0
	}
	return mask
}
