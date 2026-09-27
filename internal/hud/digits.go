package hud

import "github.com/tolgahan/ed-sense/internal/hud/vision"

// A numberFont reads one kind of HUD percentage from a straightened line.
type numberFont struct {
	glyphs *[11][vision.CellW * vision.CellH]float32 // 0-9, then "%"
	// capped numbers never pass 100%: four glyphs can only be "100%", and a
	// leading zero is unlikely. Heat goes up to 199% ("034%", "134%").
	capped bool
}

var (
	shieldFont = numberFont{glyphs: &shieldGlyphs, capped: true} // shields, hull
	heatFont   = numberFont{glyphs: &heatGlyphs}
)

const percentGlyph = 10

// read cuts the line into 2-4 glyphs ("7%" to "100%") and returns the text
// with the mean template correlation.
func (f numberFont) read(line *vision.Grid) (text string, score float64, ok bool) {
	for n := 2; n <= 4; n++ {
		w := float64(line.W)
		if w < float64(n)*0.45*vision.GlyphHeight || w > float64(n)*2.2*vision.GlyphHeight {
			continue
		}
		s := make([]byte, 0, n)
		var total float64
		for i, cell := range vision.Cells(line, n) {
			lo, hi := 0, 9
			switch {
			case i == n-1:
				lo, hi = percentGlyph, percentGlyph
			case n == 4 && f.capped:
				lo, hi = int("100"[i]-'0'), int("100"[i]-'0')
			case n == 4 && i == 0:
				lo, hi = 0, 1
			}
			best, bestScore := f.bestGlyph(cell, lo, hi)
			if best == percentGlyph {
				s = append(s, '%')
			} else {
				s = append(s, byte('0'+best))
			}
			total += bestScore
		}
		total /= float64(n)
		if f.capped && n == 3 && s[0] == '0' {
			total -= 0.25
		}
		if !ok || total > score {
			text, score, ok = string(s), total, true
		}
	}
	return text, score, ok
}

func (f numberFont) bestGlyph(cell []float64, lo, hi int) (best int, score float64) {
	best, score = lo, -2
	for k := lo; k <= hi; k++ {
		var v float64
		for j, t := range f.glyphs[k] {
			v += cell[j] * float64(t)
		}
		if v > score {
			best, score = k, v
		}
	}
	return best, score
}

// percentValue parses "NN%".
func percentValue(s string) (int, bool) {
	if len(s) < 2 || s[len(s)-1] != '%' {
		return 0, false
	}
	v := 0
	for _, ch := range s[:len(s)-1] {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		v = v*10 + int(ch-'0')
	}
	return v, true
}
