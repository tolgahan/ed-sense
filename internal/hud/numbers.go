package hud

import (
	"math"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// textThreshold: pixels scoring above it make up text.
const textThreshold = 0.3

// readLine straightens a text line of score grid g and reads it with f. The
// line is in g's coordinates; (dx, dy) is g's position in the image.
func readLine(g vision.Grid, labels []int32, line vision.TextLine, f numberFont, dx, dy int) (Number, bool) {
	p := vision.Straighten(g, labels, line)
	if p == nil {
		return Number{}, false
	}
	text, score, ok := f.read(p)
	if !ok {
		return Number{}, false
	}
	return Number{Text: text, Score: score, Box: line.Box().Offset(dx, dy), GH: line.GlyphHeight}, true
}

// numbers finds every "NN%" of colour c in r (shield font) that reads with at
// least minScore.
func numbers(im *vision.Image, r vision.Rect, screenH int, c vision.Color, m vision.Match, minScore float64) []Number {
	r = r.Clip(im.W, im.H)
	if r.W() < 8 || r.H() < 8 {
		return nil
	}
	g := vision.Score(im, r, c, m)
	labels, lines := vision.TextLines(g, screenH, textThreshold)
	var out []Number
	for _, line := range lines {
		n, ok := readLine(g, labels, line, shieldFont, r.X0, r.Y0)
		if _, valid := n.Value(); ok && valid && n.Score >= minScore {
			out = append(out, n)
		}
	}
	return out
}

// bestNear returns the best-scoring number, preferring ones within tol of (x, y).
func bestNear(ns []Number, x, y, tol float64) (Number, bool) {
	var best Number
	bestScore := -1.0
	for _, n := range ns {
		cx, cy := n.Center()
		d := math.Hypot(cx-x, cy-y) / tol
		if s := n.Score - 0.15*math.Min(1, d*d); s > bestScore {
			best, bestScore = n, s
		}
	}
	return best, bestScore > -1
}

// box is an area relative to a text, in its glyph heights.
type box struct{ x0, y0, x1, y1 float64 }

func (b box) contains(dx, dy float64) bool {
	return dx >= b.x0 && dx <= b.x1 && dy >= b.y0 && dy <= b.y1
}

// around is the area in image pixels for a text at (x, y) of glyph height
// gh, grown by mx and my glyph heights so text on the edge is whole.
func (b box) around(x, y, gh, mx, my float64) vision.Rect {
	return vision.Rect{X0: int(x + (b.x0-mx)*gh), Y0: int(y + (b.y0-my)*gh), X1: int(x + (b.x1+mx)*gh), Y1: int(y + (b.y1+my)*gh)}
}
