package hud

import "github.com/tolgahan/ed-sense/internal/hud/vision"

// readHeat reads the heat %: first right of a flame-coloured icon, else any
// heat-coloured "NN%" with an icon of another colour on its left.
func readHeat(im *vision.Image, r vision.Rect, screenH int, pal Palette) Number {
	r = r.Clip(im.W, im.H)
	if r.W() < 8 || r.H() < 8 {
		return Number{}
	}
	g := vision.Score(im, r, pal.Heat, matchHeat)
	var best Number
	take := func(n Number, ok bool) {
		if ok && (!best.Found() || n.Score > best.Score) {
			best = n
		}
	}
	// the flame is several strokes, and each finds the same text
	seen := map[vision.Rect]bool{}
	for _, flame := range flames(im, r, screenH, pal) {
		fh := float64(flame.H)
		x0, y0 := flame.X+flame.W, int(float64(flame.Y)-1.3*fh)
		if y0 < 0 {
			continue
		}
		area := vision.Rect{X0: x0, Y0: y0, X1: int(float64(x0) + 6.5*fh), Y1: int(float64(flame.Y) + 1.1*fh)}
		sub := g.Sub(area)
		if sub.W < 8 || sub.H < 8 {
			continue
		}
		labels, lines := vision.TextLines(sub, screenH, textThreshold)
		for _, line := range lines {
			if float64(line.X) >= 1.8*fh || line.GlyphHeight < 0.6*fh || line.GlyphHeight > 1.4*fh {
				continue
			}
			key := line.Box().Offset(x0, y0)
			if seen[key] {
				continue
			}
			seen[key] = true
			take(readLine(sub, labels, line, heatFont, r.X0+x0, r.Y0+y0))
		}
	}
	if best.Found() {
		return best
	}
	labels, lines := vision.TextLines(g, screenH, textThreshold)
	for _, line := range lines {
		if line.Glyphs <= 5 && iconLeft(im, r, line, pal.Heat) {
			take(readLine(g, labels, line, heatFont, r.X0, r.Y0))
		}
	}
	return best
}

// flames: the flame icon's strokes in r.
func flames(im *vision.Image, r vision.Rect, screenH int, pal Palette) []vision.Component {
	_, comps := vision.Components(vision.Mask(im, r, pal.Flame, matchFlame), r.W(), r.H())
	H := float64(screenH)
	var out []vision.Component
	for _, c := range comps {
		h := float64(c.H)
		ratio := float64(c.W) / h
		fill := float64(c.Area) / float64(c.W*c.H)
		if h < 0.006*H || h > 0.03*H || ratio < 0.3 || ratio > 1.5 || fill < 0.15 || fill > 0.75 {
			continue
		}
		out = append(out, c)
	}
	return out
}

// iconLeft: a bright blob of another colour just left of the text (the flame).
func iconLeft(im *vision.Image, r vision.Rect, line vision.TextLine, text vision.Color) bool {
	gh := line.GlyphHeight
	x0 := int(float64(line.X) - 2.2*gh)
	x1 := int(float64(line.X) - 0.05*gh)
	y0 := int(float64(line.Y) - 0.4*gh)
	y1 := int(float64(line.Y+line.H) + 0.4*gh)
	if x0 < 0 || y0 < 0 || y1 > r.H() || x1 <= x0 {
		return false
	}
	n, top, bottom := 0, y1, -1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := im.At(r.X0+x, r.Y0+y)
			if float32(max(c[0], c[1], c[2]))/255 > 0.35 && vision.ColorDistance(c, text) > 0.35 {
				n++
				top, bottom = min(top, y), max(bottom, y)
			}
		}
	}
	if float64(n) < 0.2*gh*gh || float64(n) > 2*gh*gh {
		return false
	}
	h := float64(bottom - top + 1)
	return h >= 0.5*gh && h <= 2*gh
}
