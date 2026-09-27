package vision

import (
	"math"
	"sort"
)

// TextLine is a row of glyph-sized components.
type TextLine struct {
	X, Y, W, H  int
	GlyphHeight float64 // median component height
	Glyphs      int
	ids         map[int32]bool
}

// Box is the line's bounding box.
func (l TextLine) Box() Rect { return Rect{l.X, l.Y, l.X + l.W, l.Y + l.H} }

// Center of the line's bounding box.
func (l TextLine) Center() (x, y float64) {
	return float64(l.X) + float64(l.W)/2, float64(l.Y) + float64(l.H)/2
}

// TextLines finds short lines of text (numbers such as "85%") in a score
// grid: components above threshold, 0.6-2.2% of the screen high, chained
// left to right while they sit level and close together.
func TextLines(g Grid, screenH int, threshold float32) (labels []int32, lines []TextLine) {
	mask := make([]bool, len(g.V))
	for i, v := range g.V {
		mask[i] = v > threshold
	}
	labels, all := Components(mask, g.W, g.H)
	H := float64(screenH)
	var glyphs []Component
	for _, c := range all {
		if float64(c.H) >= 0.006*H && float64(c.H) <= 0.022*H && c.W <= 4*c.H && c.FillAtLeast(0.22) {
			glyphs = append(glyphs, c)
		}
	}
	sort.SliceStable(glyphs, func(i, k int) bool { return glyphs[i].CX < glyphs[k].CX })
	used := make([]bool, len(glyphs))
	for i, c := range glyphs {
		if used[i] {
			continue
		}
		chain := []int{i}
		last := c
		for k := i + 1; k < len(glyphs); k++ {
			d := glyphs[k]
			if used[k] {
				continue
			}
			hm := float64(last.H+d.H) / 2
			if float64(d.X-(last.X+last.W)) > 0.9*hm {
				if d.CX-last.CX > 2.5*hm {
					break
				}
				continue
			}
			overlap := min(d.Y+d.H, last.Y+last.H) - max(d.Y, last.Y)
			ratio := float64(d.H) / float64(last.H)
			if math.Abs(d.CY-last.CY) < 0.4*hm && ratio > 0.65 && ratio < 1.55 && float64(overlap) >= 0.55*float64(min(d.H, last.H)) {
				chain = append(chain, k)
				last = d
			}
		}
		line := lineOf(glyphs, chain)
		if wr := float64(line.W) / line.GlyphHeight; wr >= 1.3 && wr <= 6.8 {
			for _, k := range chain {
				used[k] = true
			}
			lines = append(lines, line)
		}
	}
	return labels, lines
}

func lineOf(glyphs []Component, chain []int) TextLine {
	x0, y0, x1, y1 := math.MaxInt, math.MaxInt, -1, -1
	heights := make([]int, 0, len(chain))
	ids := map[int32]bool{}
	for _, k := range chain {
		c := glyphs[k]
		x0, y0 = min(x0, c.X), min(y0, c.Y)
		x1, y1 = max(x1, c.X+c.W), max(y1, c.Y+c.H)
		heights = append(heights, c.H)
		ids[c.ID] = true
	}
	sort.Ints(heights)
	gh := float64(heights[len(heights)/2])
	if len(heights)%2 == 0 {
		gh = float64(heights[len(heights)/2-1]+heights[len(heights)/2]) / 2
	}
	return TextLine{X: x0, Y: y0, W: x1 - x0, H: y1 - y0, GlyphHeight: gh, Glyphs: len(chain), ids: ids}
}

// Glyph cells: a straightened line is GlyphHeight pixels high and cut into
// cells of CellW x CellH for template matching.
const (
	GlyphHeight = 20
	CellW       = 12
	CellH       = 20
)

// Straighten undoes a text line's roll (from the weighted spread of its
// pixels) and italic slant (the shear with the sharpest column profile), then
// crops it and scales it to GlyphHeight pixels high. nil if too little is left.
func Straighten(score Grid, labels []int32, line TextLine) *Grid {
	x0 := max(0, int(float64(line.X)-0.6*line.GlyphHeight))
	x1 := min(score.W, int(float64(line.X+line.W)+0.6*line.GlyphHeight))
	y0 := max(0, int(float64(line.Y)-0.8*line.GlyphHeight))
	y1 := min(score.H, int(float64(line.Y+line.H)+0.8*line.GlyphHeight))
	if x1-x0 < 4 || y1-y0 < 4 {
		return nil
	}
	region, ok := lineRegion(score, labels, line, Rect{x0, y0, x1, y1})
	if !ok {
		return nil
	}
	mx, my, angle := centroidAndRoll(region)
	sheared := bestShear(region, mx, my, angle, line.GlyphHeight/GlyphHeight)
	return cropAndScale(sheared)
}

// lineRegion copies the line's own pixels (slightly grown) out of the score.
func lineRegion(score Grid, labels []int32, line TextLine, r Rect) (Grid, bool) {
	w, h := r.W(), r.H()
	keep := make([]bool, w*h)
	for y := range h {
		for x := range w {
			keep[y*w+x] = line.ids[labels[(r.Y0+y)*score.W+r.X0+x]]
		}
	}
	keep = dilate(dilate(keep, w, h), w, h)
	region := NewGrid(w, h)
	lit := 0
	for y := range h {
		for x := range w {
			if keep[y*w+x] {
				v := score.V[(r.Y0+y)*score.W+r.X0+x]
				region.V[y*w+x] = v
				if v > 0.3 {
					lit++
				}
			}
		}
	}
	return region, lit >= 8
}

// centroidAndRoll: the weighted centre of the bright pixels and the angle of
// their main axis.
func centroidAndRoll(g Grid) (mx, my, angle float64) {
	var sw float64
	for y := range g.H {
		for x := range g.W {
			if v := float64(g.V[y*g.W+x]); v > 0.3 {
				sw += v
				mx += float64(x) * v
				my += float64(y) * v
			}
		}
	}
	mx /= sw
	my /= sw
	var cxx, cyy, cxy float64
	for y := range g.H {
		for x := range g.W {
			v := float64(g.V[y*g.W+x])
			if v <= 0.3 {
				continue
			}
			dx, dy := float64(x)-mx, float64(y)-my
			cxx += dx * dx * v
			cyy += dy * dy * v
			cxy += dx * dy * v
		}
	}
	angle = 0.5 * math.Atan2(2*cxy/sw, cxx/sw-cyy/sw)
	return mx, my, math.Max(-0.6, math.Min(0.6, angle))
}

// bestShear resamples the region along its roll at scale s per output pixel,
// trying 21 italic shears and keeping the sharpest column profile.
func bestShear(region Grid, mx, my, angle, s float64) Grid {
	ux, uy := math.Cos(angle), math.Sin(angle)
	vx, vy := -uy, ux
	dx, dy := ux*s, uy*s // source step per output pixel along the line
	L := int(float64(region.W)/s) + 4
	Hh := int(1.8 * GlyphHeight)
	best, cur := NewGrid(L, Hh), NewGrid(L, Hh)
	bestSharpness := -1.0
	col := make([]float64, L)
	for ki := range 21 {
		k := -0.5 + 0.05*float64(ki)
		clear(col)
		for j := range Hh {
			pv := float64(j) - float64(Hh)/2
			pu := -float64(L)/2 + k*pv
			sx := mx + (pu*ux+pv*vx)*s
			sy := my + (pu*uy+pv*vy)*s
			row := cur.V[j*L : (j+1)*L]
			for i := range row {
				v := bilinear(region, sx+float64(i)*dx, sy+float64(i)*dy)
				row[i] = v
				col[i] += float64(v)
			}
		}
		var sharpness float64
		for _, c := range col {
			sharpness += c * c
		}
		if sharpness > bestSharpness {
			bestSharpness = sharpness
			best, cur = cur, best
		}
	}
	return best
}

func cropAndScale(p Grid) *Grid {
	rows := make([]float64, p.H)
	cols := make([]float64, p.W)
	for j := range p.H {
		for i := range p.W {
			v := float64(p.V[j*p.W+i])
			rows[j] += v
			cols[i] += v
		}
	}
	r0, r1, okr := spanAbove(rows, 0.25)
	c0, c1, okc := spanAbove(cols, 0.12)
	if !okr || !okc || r1-r0 < 3 || c1-c0 < 3 {
		return nil
	}
	crop := NewGrid(c1-c0, r1-r0)
	for j := r0; j < r1; j++ {
		copy(crop.V[(j-r0)*crop.W:(j-r0+1)*crop.W], p.V[j*p.W+c0:j*p.W+c1])
	}
	w := max(4, int(math.Round(float64(crop.W)*GlyphHeight/float64(crop.H))))
	out := resize(crop, w, GlyphHeight)
	return &out
}

// bilinear samples g at (sx, sy), 0 outside.
func bilinear(g Grid, sx, sy float64) float32 {
	x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
	fx, fy := float32(sx-float64(x0)), float32(sy-float64(y0))
	if x0 >= 0 && y0 >= 0 && x0+1 < g.W && y0+1 < g.H {
		i := y0*g.W + x0
		a, b, c, d := g.V[i], g.V[i+1], g.V[i+g.W], g.V[i+g.W+1]
		return a*(1-fx)*(1-fy) + b*fx*(1-fy) + c*(1-fx)*fy + d*fx*fy
	}
	tap := func(x, y int) float32 {
		if x < 0 || y < 0 || x >= g.W || y >= g.H {
			return 0
		}
		return g.V[y*g.W+x]
	}
	return tap(x0, y0)*(1-fx)*(1-fy) + tap(x0+1, y0)*fx*(1-fy) + tap(x0, y0+1)*(1-fx)*fy + tap(x0+1, y0+1)*fx*fy
}

// resize scales g to w x h with bilinear sampling.
func resize(g Grid, w, h int) Grid {
	out := NewGrid(w, h)
	for y := range h {
		sy := math.Max(0, math.Min(float64(g.H-1), (float64(y)+0.5)*float64(g.H)/float64(h)-0.5))
		y0 := int(math.Floor(sy))
		y1 := min(y0+1, g.H-1)
		fy := sy - float64(y0)
		for x := range w {
			sx := math.Max(0, math.Min(float64(g.W-1), (float64(x)+0.5)*float64(g.W)/float64(w)-0.5))
			x0 := int(math.Floor(sx))
			x1 := min(x0+1, g.W-1)
			fx := sx - float64(x0)
			v := float64(g.At(x0, y0))*(1-fx)*(1-fy) + float64(g.At(x1, y0))*fx*(1-fy) +
				float64(g.At(x0, y1))*(1-fx)*fy + float64(g.At(x1, y1))*fx*fy
			out.V[y*w+x] = float32(v)
		}
	}
	return out
}

// spanAbove is [first, last+1) of the entries above frac of the maximum.
func spanAbove(p []float64, frac float64) (int, int, bool) {
	peak := 0.0
	for _, v := range p {
		peak = math.Max(peak, v)
	}
	if peak <= 0 {
		return 0, 0, false
	}
	a, b := -1, -1
	for i, v := range p {
		if v > frac*peak {
			if a < 0 {
				a = i
			}
			b = i
		}
	}
	return a, b + 1, a >= 0
}

// boxWeights: output pixel o averages input [o*s, (o+1)*s), with fractional
// overlap at the edges.
func boxWeights(nIn, nOut int) [][]float64 {
	w := make([][]float64, nOut)
	scale := float64(nIn) / float64(nOut)
	for o := range nOut {
		w[o] = make([]float64, nIn)
		a, b := float64(o)*scale, float64(o+1)*scale
		var sum float64
		for i := int(math.Floor(a)); i < min(nIn, int(math.Ceil(b))); i++ {
			if ov := math.Min(b, float64(i+1)) - math.Max(a, float64(i)); ov > 0 {
				w[o][i] = ov
				sum += ov
			}
		}
		for i := range w[o] {
			w[o][i] /= sum
		}
	}
	return w
}

// Cells cuts a straightened line into n equal glyph cells of CellW x CellH,
// each zero-mean and unit-norm, ready for correlation with templates.
func Cells(line *Grid, n int) [][]float64 {
	out := make([][]float64, n)
	wy := boxWeights(line.H, CellH)
	for i := range n {
		a, b := i*line.W/n, (i+1)*line.W/n
		if b <= a {
			b = a + 1
		}
		wx := boxWeights(b-a, CellW)
		c := make([]float64, CellW*CellH)
		for oy := range CellH {
			for ox := range CellW {
				var v float64
				for y := range line.H {
					if wy[oy][y] == 0 {
						continue
					}
					var row float64
					for x := range b - a {
						if wx[ox][x] != 0 {
							row += wx[ox][x] * float64(line.V[y*line.W+a+x])
						}
					}
					v += wy[oy][y] * row
				}
				c[oy*CellW+ox] = v
			}
		}
		normalise(c)
		out[i] = c
	}
	return out
}

func normalise(c []float64) {
	var mean float64
	for _, v := range c {
		mean += v
	}
	mean /= float64(len(c))
	var norm float64
	for i := range c {
		c[i] -= mean
		norm += c[i] * c[i]
	}
	if norm = math.Sqrt(norm); norm > 1e-6 {
		for i := range c {
			c[i] /= norm
		}
	}
}

// Percentile with linear interpolation (numpy's default).
func Percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	a := append([]float64(nil), v...)
	sort.Float64s(a)
	pos := float64(len(a)-1) * p / 100
	lo, hi := int(math.Floor(pos)), int(math.Ceil(pos))
	return a[lo] + (a[hi]-a[lo])*(pos-float64(lo))
}
