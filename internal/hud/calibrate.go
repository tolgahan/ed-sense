package hud

import (
	"math"
	"sort"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// Finding the HUD colours on screen, for HUDs recoloured beyond what the
// colour matrix predicts (EDHM themes and the like). The right-hand panel
// always has two "NN%": the shield % in the shield colour, and down left of
// it the hull % in the HUD's main colour. Every strong colour on the panel is
// tried as a text colour; the pair of reads in that layout gives both colours.

// Calibration is what was found on the right-hand panel.
type Calibration struct {
	OK           bool
	Shield, Main vision.Color
	Heat         vision.Color
	Score        float64
	ShieldText   string
	HullText     string
}

// calibrate looks for the shield % / hull % pair in r.
func calibrate(im *vision.Image, r vision.Rect, screenH int) Calibration {
	r = r.Clip(im.W, im.H)
	type colourReads struct {
		c     vision.Color
		reads []Number
	}
	var all []colourReads
	for _, c := range candidateColours(im, r, 8) {
		if ns := numbers(im, r, screenH, c, matchCalibration, 0.7); len(ns) > 0 {
			all = append(all, colourReads{c, ns})
		}
	}
	var best Calibration
	var shieldBox, hullBox vision.Rect
	for _, s := range all {
		for _, m := range all {
			if s.c == m.c || vision.ColorDistance(s.c, m.c) < 0.2 {
				continue
			}
			for _, a := range s.reads {
				for _, b := range m.reads {
					ax, ay := a.Center()
					bx, by := b.Center()
					g := (a.GH + b.GH) / 2
					dx, dy := (ax-bx)/g, (ay-by)/g
					if dx < 3 || dx > 16 || dy < -10 || dy > -2 {
						continue
					}
					if score := a.Score + b.Score; !best.OK || score > best.Score {
						best = Calibration{OK: true, Shield: s.c, Main: m.c, Score: score, ShieldText: a.Text, HullText: b.Text}
						shieldBox, hullBox = a.Box, b.Box
					}
				}
			}
		}
	}
	if best.OK {
		// from the colour group to the colour of the text itself
		best.Shield = textColour(im, shieldBox, best.Shield)
		best.Main = textColour(im, hullBox, best.Main)
	}
	return best
}

// calibrateHeat finds the heat colour in r: it shares the main colour's
// family, so the strong colours near it are tried and the one that reads best
// is kept. When none reads, it returns main and false.
func calibrateHeat(im *vision.Image, r vision.Rect, screenH int, main vision.Color, base Palette) (vision.Color, bool) {
	r = r.Clip(im.W, im.H)
	best, bestScore, found := main, 0.0, false
	// the expected heat colour and the main colour first: on a tie they win
	cands := append([]vision.Color{base.Heat, main}, candidateColours(im, r, 8)...)
	for _, c := range cands {
		if vision.ColorDistance(c, main) > 0.45 {
			continue
		}
		p := base
		p.Heat = c
		heat := readHeat(im, r, screenH, p)
		if _, ok := heat.Value(); ok && heat.Score >= 0.7 && (!found || heat.Score > bestScore) {
			best, bestScore, found = c, heat.Score, true
		}
	}
	return best, found
}

// CalibrateImage looks for the HUD colours in a whole game window. base is
// the palette read with so far.
func CalibrateImage(im *vision.Image, base Palette) Calibration {
	c := calibrate(im, shieldArea(im.W, im.H), im.H)
	if c.OK {
		base.Shield = c.Shield
		c.Heat, _ = calibrateHeat(im, heatArea(im.W, im.H), im.H, c.Main, base)
	}
	return c
}

// Agrees reports whether two calibrations found the same colours.
func (c Calibration) Agrees(o Calibration) bool {
	return c.OK && o.OK && vision.ColorDistance(c.Shield, o.Shield) < 0.15 && vision.ColorDistance(c.Heat, o.Heat) < 0.2
}

// candidateColours: the k dominant bright colours in r (24 hue bins over
// saturated pixels plus one for white and grey). Each bin's colour comes from
// its most saturated, brightest pixels: the core of text strokes.
func candidateColours(im *vision.Image, r vision.Rect, k int) []vision.Color {
	type pixel struct{ r, g, b, key float32 }
	bins := make([][]pixel, 25)
	for y := r.Y0; y < r.Y1; y++ {
		for x := r.X0; x < r.X1; x++ {
			rr, gg, bb := im.RGB(x, y)
			hi := max(rr, gg, bb)
			if hi <= 0.35 {
				continue
			}
			sat := (hi - min(rr, gg, bb)) / hi
			bin, key := 24, hi
			if sat > 0.25 {
				h, _, _ := vision.HSV(rr, gg, bb)
				bin, key = int(h/15)%24, sat*hi
			}
			bins[bin] = append(bins[bin], pixel{rr, gg, bb, key})
		}
	}
	type candidate struct {
		n int
		c vision.Color
	}
	var cands []candidate
	for _, list := range bins {
		if len(list) < 50 {
			continue
		}
		keys := make([]float64, len(list))
		for i, p := range list {
			keys[i] = float64(p.key)
		}
		threshold := vision.Percentile(keys, 70)
		var rs, gs, bs []float64
		for _, p := range list {
			if float64(p.key) >= threshold {
				rs, gs, bs = append(rs, float64(p.r)), append(gs, float64(p.g)), append(bs, float64(p.b))
			}
		}
		c := vision.Color{uint8(vision.Percentile(rs, 50) * 255), uint8(vision.Percentile(gs, 50) * 255), uint8(vision.Percentile(bs, 50) * 255)}
		cands = append(cands, candidate{len(list), c})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].n != cands[j].n {
			return cands[i].n > cands[j].n
		}
		for ch := range 3 {
			if cands[i].c[ch] != cands[j].c[ch] {
				return cands[i].c[ch] > cands[j].c[ch]
			}
		}
		return false
	})
	var out []vision.Color
	for i := 0; i < len(cands) && i < k; i++ {
		out = append(out, cands[i].c)
	}
	return out
}

// textColour: the median colour of the brighter half of a text's matching pixels.
func textColour(im *vision.Image, box vision.Rect, c vision.Color) vision.Color {
	box = box.Clip(im.W, im.H)
	g := vision.Score(im, box, c, matchCalibration)
	var rs, gs, bs, vs []float64
	for y := range box.H() {
		for x := range box.W() {
			if g.V[y*g.W+x] <= 0.5 {
				continue
			}
			p := im.At(box.X0+x, box.Y0+y)
			r, gg, b := float64(p[0]), float64(p[1]), float64(p[2])
			rs, gs, bs = append(rs, r), append(gs, gg), append(bs, b)
			vs = append(vs, math.Max(r, math.Max(gg, b)))
		}
	}
	if len(vs) < 10 {
		return c
	}
	median := vision.Percentile(vs, 50)
	var hr, hg, hb []float64
	for i, v := range vs {
		if v >= median {
			hr, hg, hb = append(hr, rs[i]), append(hg, gs[i]), append(hb, bs[i])
		}
	}
	return vision.Color{uint8(vision.Percentile(hr, 50)), uint8(vision.Percentile(hg, 50)), uint8(vision.Percentile(hb, 50))}
}
