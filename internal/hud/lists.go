package hud

import (
	"fmt"
	"math"
	"sort"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// The fire group lists on the cockpit struts: PRIMARY (R2) on the right,
// SECONDARY (L2) on the left. Each entry is a module name (weapons carry a
// mount icon at the outer end) with a thin segmented bar under it: the clip
// for modules with ammo, lit from the right, the used part dim. Modules with
// ammo have "clip/reserve" under the bar, which reads RELOADING while the
// clip refills (bar all dim) and DEPLOYING while the hardpoints come out.
// OUT OF RANGE turns the whole entry red.
//
// The lists are tilted, foreshortened and sway with the pilot's head, and the
// text (about 0.8% of the screen high) is too small to read letter by letter.
// Each entry is matched against the ship's modules by its width, the mount
// icon and its ammo line (see names.go).

// ListRead is one read of a list.
type ListRead struct {
	OK      bool // the list's area was captured and searched
	Entries []Entry
}

// Entry is one entry of a list, in image pixels.
type Entry struct {
	X0, Y0, X1, Y1 float64 // the name's box
	G              float64 // glyph height
	Width          float64 // name width in glyph heights, icon included
	Icon           bool    // mount icon at the outer end: a weapon
	Red            bool    // out of range
	Sub            SubLine // the line under the bar
	SubWidth       float64 // its width in its glyph heights
	Clip           float64 // lit part of the bar, 0-1
}

// SubLine is what the line under an entry's bar shows.
type SubLine int

const (
	NoSubLine  SubLine = iota
	AmmoLine           // "clip/reserve"
	StatusLine         // a status word: RELOADING, DEPLOYING
)

// pixel classes in a list
const (
	pxNone   = iota
	pxDim    // the main colour, dim: a bar's used part, glow
	pxLit    // the main colour at text level
	pxFull   // the main colour, bright: text cores, a bar's charged part
	pxRedDim // out of range
	pxRedLit
)

var (
	matchListFull   = vision.Match{HueWidth: 9, MinSat: 0.5, MinVal: 0.85, ChromaTol: 0.3} // a bar's used part is darker and redder
	matchListDim    = vision.Match{HueWidth: 26, MinSat: 0.5, MinVal: 0.3, ChromaTol: 0.5} // excludes the panel's tinted glass
	matchListRedLit = vision.Match{HueWidth: 16, MinSat: 0.6, MinVal: 0.9, ChromaTol: 0.35}
	matchListRedDim = vision.Match{HueWidth: 20, MinSat: 0.6, MinVal: 0.45, ChromaTol: 0.35}
)

// listClasses classifies r: the main colour (bright, text, dim) and the red
// of out-of-range entries (the heat flame's red, so it follows colour mods).
func listClasses(im *vision.Image, r vision.Rect, pal Palette) []uint8 {
	full, _ := vision.Matcher(pal.Main, matchListFull, true)
	lit, _ := vision.Matcher(pal.Main, matchHeat, true)
	dim, _ := vision.Matcher(pal.Main, matchListDim, true)
	redLit, _ := vision.Matcher(pal.Flame, matchListRedLit, true)
	redDim, _ := vision.Matcher(pal.Flame, matchListRedDim, true)
	return vision.Classify(im, r, fmt.Sprintf("lists-%v-%v", pal.Main, pal.Flame), func(r, g, b float32) uint8 {
		switch {
		case full(r, g, b) > 0:
			return pxFull
		case lit(r, g, b) > 0:
			return pxLit
		case redLit(r, g, b) > 0:
			return pxRedLit
		case dim(r, g, b) > 0:
			return pxDim
		case redDim(r, g, b) > 0:
			return pxRedDim
		}
		return pxNone
	})
}

// listLine is a row of glyph-like components.
type listLine struct {
	comps          []vision.Component
	x0, x1, y0, y1 float64
	g              float64 // glyph height
	slope, base    float64 // the letters' bottom: y = base + slope*x
	red            bool
}

func (l *listLine) bottom(x float64) float64 { return l.base + l.slope*x }

// readList reads the list in r.
func readList(im *vision.Image, r vision.Rect, screenH int, pal Palette, right bool) ListRead {
	r = r.Clip(im.W, im.H)
	W, H := r.W(), r.H()
	if W < 16 || H < 16 {
		return ListRead{}
	}
	classes := listClasses(im, r, pal)
	comps, red := listGlyphs(classes, W, H, screenH)
	lines := listLines(comps, red)

	// entries: lines with a bar under them, top down; the line under a bar
	// belongs to the entry above
	type entry struct {
		line      *listLine
		off, clip float64
	}
	var entries []entry
	inEntry := map[int32]bool{}
	sort.Slice(lines, func(a, b int) bool { return lines[a].y0 < lines[b].y0 })
	for i := range lines {
		l := &lines[i]
		// lines touching the edge may be cut short
		if l.x0 <= 1 || l.y0 <= 1 || l.x1 >= float64(W-1) || l.y1 >= float64(H-1) {
			continue
		}
		cx, cy := (l.x0+l.x1)/2, (l.y0+l.y1)/2
		under := false
		for _, e := range entries {
			yb := e.line.bottom(cx) + e.off
			if cy > yb && cy < yb+2*e.line.g && cx > e.line.x0-e.line.g && cx < e.line.x1+e.line.g {
				under = true
				break
			}
		}
		if under {
			continue
		}
		if off, clip, ok := barBelow(l, classes, W, H); ok {
			entries = append(entries, entry{l, off, clip})
			for _, c := range l.comps {
				inEntry[c.ID] = true
			}
		}
	}

	out := ListRead{OK: true}
	for _, f := range entries {
		l := f.line
		e := Entry{
			X0: l.x0 + float64(r.X0), Y0: l.y0 + float64(r.Y0), X1: l.x1 + float64(r.X0), Y1: l.y1 + float64(r.Y0),
			G: l.g, Width: (l.x1 - l.x0) / l.g, Icon: iconAtEnd(l, right), Red: l.red, Clip: f.clip,
		}
		e.Sub, e.SubWidth = subLine(l, f.off, comps, inEntry)
		out.Entries = append(out.Entries, e)
	}
	sort.Slice(out.Entries, func(a, b int) bool { return out.Entries[a].Y0 < out.Entries[b].Y0 })
	return out
}

// listGlyphs: text-sized components of the lit classes, and whether each is red.
func listGlyphs(classes []uint8, W, H, screenH int) ([]vision.Component, []bool) {
	mask := make([]bool, len(classes))
	for i, c := range classes {
		mask[i] = c == pxLit || c == pxFull || c == pxRedLit
	}
	labels, all := vision.Components(mask, W, H)
	sh := float64(screenH)
	var comps []vision.Component
	var red []bool
	for _, c := range all {
		if float64(c.H) < 0.005*sh || float64(c.H) > 0.022*sh || c.W > 8*c.H || !c.FillAtLeast(0.2) {
			continue
		}
		nRed := 0
		for y := c.Y; y < c.Y+c.H; y++ {
			for x := c.X; x < c.X+c.W; x++ {
				if labels[y*W+x] == c.ID && classes[y*W+x] == pxRedLit {
					nRed++
				}
			}
		}
		comps = append(comps, c)
		red = append(red, nRed*2 > c.Area)
	}
	return comps, red
}

// listLines chains components into text lines, left to right.
func listLines(comps []vision.Component, red []bool) []listLine {
	order := make([]int, len(comps))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return comps[order[a]].CX < comps[order[b]].CX })
	used := make([]bool, len(comps))
	var out []listLine
	for oi, i := range order {
		if used[i] {
			continue
		}
		chain := []int{i}
		last := comps[i]
		for _, j := range order[oi+1:] {
			if used[j] {
				continue
			}
			d := comps[j]
			hm := float64(last.H+d.H) / 2
			gap := float64(d.X - (last.X + last.W))
			if gap > 1.3*hm {
				if d.CX-last.CX > 3*hm {
					break
				}
				continue
			}
			ratio := float64(d.H) / float64(last.H)
			if math.Abs(d.CY-last.CY) < 0.35*hm && ratio > 0.6 && ratio < 1.6 && gap > -0.3*hm {
				chain = append(chain, j)
				last = d
			}
		}
		if len(chain) < 3 {
			continue
		}
		if l, ok := newListLine(comps, red, chain); ok {
			for _, k := range chain {
				used[k] = true
			}
			out = append(out, l)
		}
	}
	return out
}

func newListLine(comps []vision.Component, red []bool, chain []int) (listLine, bool) {
	l := listLine{x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
	nRed := 0
	for _, k := range chain {
		c := comps[k]
		l.comps = append(l.comps, c)
		l.x0, l.y0 = math.Min(l.x0, float64(c.X)), math.Min(l.y0, float64(c.Y))
		l.x1, l.y1 = math.Max(l.x1, float64(c.X+c.W)), math.Max(l.y1, float64(c.Y+c.H))
		if red[k] {
			nRed++
		}
	}
	l.red = nRed*2 > len(chain)
	// roll: the centres' slope; height: bounding boxes grow with the slope,
	// by about a third of the glyph width
	var sx, sy, sxx, sxy float64
	for _, c := range l.comps {
		sx += c.CX
		sy += c.CY
		sxx += c.CX * c.CX
		sxy += c.CX * c.CY
	}
	n := float64(len(l.comps))
	if d := n*sxx - sx*sx; d > 1e-9 {
		l.slope = math.Max(-0.4, math.Min(0.4, (n*sxy-sx*sy)/d))
	}
	heights := make([]float64, 0, len(l.comps))
	bottoms := make([]float64, 0, len(l.comps))
	for _, c := range l.comps {
		heights = append(heights, float64(c.H)-0.35*float64(c.W)*math.Abs(l.slope))
		bottoms = append(bottoms, float64(c.Y+c.H)-l.slope*c.CX)
	}
	l.g = vision.Percentile(heights, 50)
	l.base = vision.Percentile(bottoms, 50)
	return l, l.g >= 3 && (l.x1-l.x0)/l.g >= 2.5
}

// barBelow looks for an entry's bar under line l: a thin row of segments (lit
// or dim) along the text, 0.1-1.7 glyph heights below it. It returns the
// bar's offset below the letters and its lit part.
func barBelow(l *listLine, classes []uint8, W, H int) (off, lit float64, ok bool) {
	g := l.g
	span := l.x1 - l.x0
	// the middle of the name: the bar is under it whether the icon is left or right
	c0, c1 := int(l.x0+0.2*span), int(l.x1-0.2*span)
	if c1-c0 < 4 {
		return 0, 0, false
	}
	o0, o1 := max(1, int(0.1*g)), int(1.7*g)+1
	cover := make([]float64, o1+1)
	best, bestO := 0.0, -1
	for o := o0; o <= o1; o++ {
		hits := 0
		for x := c0; x < c1; x++ {
			y := int(math.Round(l.bottom(float64(x)) + float64(o)))
			if y >= 0 && y < H && x >= 0 && x < W && classes[y*W+x] != pxNone {
				hits++
			}
		}
		cover[o] = float64(hits) / float64(c1-c0)
		if cover[o] > best {
			best, bestO = cover[o], o
		}
	}
	if best < 0.55 {
		return 0, 0, false
	}
	// thin: the rows around the peak that are nearly as full
	a, b := bestO, bestO
	for a > o0 && cover[a-1] >= 0.5*best {
		a--
	}
	for b < o1 && cover[b+1] >= 0.5*best {
		b++
	}
	if float64(b-a+1) > 0.62*g || a == o0 && cover[o0] >= 0.5*best {
		return 0, 0, false // a text line or a panel edge
	}
	// the bar's length and lit part: from the middle outwards while it goes on
	rows := b - a + 1
	column := func(x int) (anyN, litN int) {
		for o := a; o <= b; o++ {
			y := int(math.Round(l.bottom(float64(x)) + float64(o)))
			if y < 0 || y >= H || x < 0 || x >= W {
				continue
			}
			switch classes[y*W+x] {
			case pxFull, pxRedLit:
				anyN++
				litN++
			case pxLit, pxDim, pxRedDim:
				anyN++
			}
		}
		return anyN, litN
	}
	maxMiss := max(2, int(0.5*g))
	var ends [2]int
	var nLit, nAny int
	for k, dir := range []int{-1, 1} {
		miss := 0
		x := (c0 + c1) / 2
		if dir > 0 {
			x++
		}
		ends[k] = x
		for ; x >= 0 && x < W && miss <= maxMiss; x += dir {
			an, li := column(x)
			if an*2 < rows {
				miss++
				continue
			}
			miss = 0
			ends[k] = x
			nAny += an
			nLit += li
		}
	}
	// about as long as the name; much longer is a panel edge running on
	if n := float64(ends[1] - ends[0]); nAny == 0 || n > span+3.5*g || n < 0.4*span {
		return 0, 0, false
	}
	return float64(a+b) / 2, float64(nLit) / float64(nAny), true
}

// iconAtEnd: a mount icon at the outer end of the line, taller than the
// letters and set apart.
func iconAtEnd(l *listLine, right bool) bool {
	n := len(l.comps)
	if n < 3 {
		return false
	}
	c, next := l.comps[0], l.comps[1]
	gap := float64(next.X - (c.X + c.W))
	if right {
		c, next = l.comps[n-1], l.comps[n-2]
		gap = float64(c.X - (next.X + next.W))
	}
	h := float64(c.H) - 0.35*float64(c.W)*math.Abs(l.slope)
	w := float64(c.W)
	return h >= 1.1*l.g && w >= 0.85*l.g && w <= 1.6*l.g && gap >= 0.25*l.g
}

// subLine finds the line under an entry's bar among the components not in
// an entry: an ammo count, or a status word (long, letters close together).
func subLine(l *listLine, barOff float64, comps []vision.Component, inEntry map[int32]bool) (SubLine, float64) {
	var sub []vision.Component
	for _, c := range comps {
		if inEntry[c.ID] {
			continue
		}
		bar := l.bottom(c.CX) + barOff
		if c.CY < bar+0.3*l.g || c.CY > bar+1.9*l.g || c.CX < l.x0-0.3*l.g || c.CX > l.x1+0.3*l.g {
			continue
		}
		if h := float64(c.H); h < 0.55*l.g || h > 1.35*l.g {
			continue
		}
		sub = append(sub, c)
	}
	if len(sub) < 2 {
		return NoSubLine, 0
	}
	sort.Slice(sub, func(a, b int) bool { return sub[a].CX < sub[b].CX })
	heights := make([]float64, len(sub))
	x0, x1, maxGap := math.Inf(1), math.Inf(-1), 0.0
	for k, c := range sub {
		heights[k] = float64(c.H)
		x0, x1 = math.Min(x0, float64(c.X)), math.Max(x1, float64(c.X+c.W))
		if k > 0 {
			maxGap = math.Max(maxGap, float64(c.X-(sub[k-1].X+sub[k-1].W)))
		}
	}
	g := vision.Percentile(heights, 50)
	width := (x1 - x0) / g
	if len(sub) >= 6 && maxGap <= 0.6*g && width >= 7 {
		return StatusLine, width
	}
	return AmmoLine, width
}
