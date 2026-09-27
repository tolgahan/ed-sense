package vision

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

// Color is an RGB colour.
type Color [3]uint8

func (c Color) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

func (c Color) floats() (r, g, b float32) {
	return float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255
}

// HSV of the colour: hue in degrees, saturation and value 0-1.
func (c Color) HSV() (h, s, v float32) { return HSV(c.floats()) }

// ParseHex reads "#rrggbb".
func ParseHex(s string) (Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return Color{}, false
	}
	return Color{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
}

// HSV converts 0-1 RGB to hue (degrees), saturation and value.
func HSV(r, g, b float32) (h, s, v float32) {
	hi, lo := max(r, g, b), min(r, g, b)
	d := hi - lo
	if d > 1e-6 {
		switch hi {
		case r:
			h = (g - b) / d
			if h < 0 {
				h += 6
			}
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h *= 60
	}
	if hi > 1e-6 {
		s = d / hi
	}
	return h, s, hi
}

// HueDistance in degrees, 0-180.
func HueDistance(a, b float32) float32 {
	d := a - b
	if d < 0 {
		d = -d
	}
	return min(d, 360-d)
}

// chroma is the colour divided by its brightest channel: it tells colours
// apart regardless of brightness, and works for pale colours with no
// reliable hue.
func chroma(r, g, b float32) (cr, cg, cb, v float32) {
	v = max(r, g, b)
	d := max(v, 1e-6)
	return r / d, g / d, b / d, v
}

func chromaDistance(r1, g1, b1, r2, g2, b2 float32) float32 {
	return float32(math.Sqrt(float64((r1-r2)*(r1-r2) + (g1-g2)*(g1-g2) + (b1-b2)*(b1-b2))))
}

// ColorDistance: how different two colours are, ignoring brightness.
func ColorDistance(a, b Color) float32 {
	a1, a2, a3, _ := chroma(a.floats())
	b1, b2, b3, _ := chroma(b.floats())
	return chromaDistance(a1, a2, a3, b1, b2, b3)
}

// Match is how close a pixel must be to a target colour. Saturated targets
// are matched by hue, with saturation and brightness relative to the
// target's, so a colour and its dimmer glow match alike. Pale targets
// (white or grey HUDs) have no reliable hue and are matched by chroma.
type Match struct {
	HueWidth  float32 // degrees either side of the target hue
	MinSat    float32 // saturation at least this fraction of the target's
	MinVal    float32 // brightness at least this fraction of the target's
	ChromaTol float32 // chroma distance, for pale targets
	// ByChroma always matches by chroma. Colour calibration uses it: it picks
	// text of an unknown colour out of a panel of similar hues, and separates
	// a pale text from a deeper background of the same hue.
	ByChroma bool
}

// paleSaturation: below it a target is matched by chroma.
const paleSaturation = 0.3

// Matcher returns a pixel scorer for target (0-1, soft edges; hard: 0 or 1)
// and the brightness below which every pixel scores 0.
func Matcher(target Color, m Match, hard bool) (score func(r, g, b float32) float32, minValue float32) {
	th, ts, tv := target.HSV()
	if ts >= paleSaturation && !m.ByChroma {
		minSat, minVal := m.MinSat*ts, m.MinVal*tv
		if hard {
			return func(r, g, b float32) float32 {
				h, s, v := HSV(r, g, b)
				if v > minVal && s > minSat && HueDistance(h, th) < m.HueWidth {
					return 1
				}
				return 0
			}, minVal
		}
		return func(r, g, b float32) float32 {
			h, s, v := HSV(r, g, b)
			return clamp01(1-HueDistance(h, th)/m.HueWidth) * clamp01((s-minSat)/(0.37*ts)) * clamp01((v-minVal)/(0.37*tv))
		}, minVal
	}
	tr, tg, tb, _ := chroma(target.floats())
	minVal := m.MinVal * tv
	if hard {
		return func(r, g, b float32) float32 {
			cr, cg, cb, v := chroma(r, g, b)
			if v > minVal && chromaDistance(cr, cg, cb, tr, tg, tb) < m.ChromaTol {
				return 1
			}
			return 0
		}, minVal
	}
	return func(r, g, b float32) float32 {
		cr, cg, cb, v := chroma(r, g, b)
		return clamp01(1-chromaDistance(cr, cg, cb, tr, tg, tb)/m.ChromaTol) * clamp01((v-minVal)/(0.4*tv))
	}, minVal
}

func clamp01(x float32) float32 { return min(max(x, 0), 1) }

// Scoring every pixel through HSV is the reader's main cost, so each
// (colour, match) pair gets a lookup table over RGB at 6 bits per channel
// (256 KB, built once in a few ms).
type lut struct {
	v    []uint8 // score * 255 at (r>>2)<<12 | (g>>2)<<6 | b>>2
	skip int     // pixels with every channel at or below this score 0
}

type lutKey struct {
	target Color
	m      Match
	hard   bool
}

// Colour calibration tries many colours; the caches are kept bounded.
const (
	matchLUTLimit = 24
	classLUTLimit = 16
)

var (
	lutMu     sync.Mutex
	matchLUTs = map[lutKey]*lut{}
	classLUTs = map[string]*lut{}
)

func lutIndex(r, g, b int) int { return r>>2<<12 | g>>2<<6 | b>>2 }

func buildLUT(f func(r, g, b float32) uint8) []uint8 {
	v := make([]uint8, 1<<18)
	for i := range v {
		v[i] = f(float32((i>>12)<<2+2)/255, float32((i>>6&63)<<2+2)/255, float32((i&63)<<2+2)/255)
	}
	return v
}

func matchLUT(target Color, m Match, hard bool) *lut {
	k := lutKey{target, m, hard}
	lutMu.Lock()
	defer lutMu.Unlock()
	if l := matchLUTs[k]; l != nil {
		return l
	}
	if len(matchLUTs) >= matchLUTLimit {
		matchLUTs = map[lutKey]*lut{}
	}
	f, minVal := Matcher(target, m, hard)
	l := &lut{v: buildLUT(func(r, g, b float32) uint8 { return uint8(f(r, g, b)*255 + 0.5) }), skip: int(minVal * 255)}
	matchLUTs[k] = l
	return l
}

// Score rates how much each pixel in r looks like target (0-1).
func Score(im *Image, r Rect, target Color, m Match) Grid {
	g := NewGrid(r.W(), r.H())
	l := matchLUT(target, m, false)
	for y := range g.H {
		row := (r.Y0+y)*im.Stride + r.X0*4
		out := g.V[y*g.W : (y+1)*g.W]
		for x := range out {
			i := row + x*4
			b, gr, rd := int(im.Pix[i]), int(im.Pix[i+1]), int(im.Pix[i+2])
			if b <= l.skip && gr <= l.skip && rd <= l.skip {
				continue
			}
			if v := l.v[lutIndex(rd, gr, b)]; v != 0 {
				out[x] = float32(v) / 255
			}
		}
	}
	return g
}

// Mask marks the pixels in r that match target.
func Mask(im *Image, r Rect, target Color, m Match) []bool {
	out := make([]bool, r.W()*r.H())
	l := matchLUT(target, m, true)
	for y := range r.H() {
		row := (r.Y0+y)*im.Stride + r.X0*4
		for x := range r.W() {
			i := row + x*4
			b, gr, rd := int(im.Pix[i]), int(im.Pix[i+1]), int(im.Pix[i+2])
			if b <= l.skip && gr <= l.skip && rd <= l.skip {
				continue
			}
			out[y*r.W()+x] = l.v[lutIndex(rd, gr, b)] != 0
		}
	}
	return out
}

// Classify gives every pixel in r a class (0-255) from classOf, through a
// lookup table cached under key.
func Classify(im *Image, r Rect, key string, classOf func(r, g, b float32) uint8) []uint8 {
	lutMu.Lock()
	l := classLUTs[key]
	if l == nil {
		if len(classLUTs) >= classLUTLimit {
			classLUTs = map[string]*lut{}
		}
		l = &lut{v: buildLUT(classOf)}
		classLUTs[key] = l
	}
	lutMu.Unlock()
	out := make([]uint8, r.W()*r.H())
	for y := range r.H() {
		row := (r.Y0+y)*im.Stride + r.X0*4
		o := out[y*r.W() : (y+1)*r.W()]
		for x := range o {
			i := row + x*4
			o[x] = l.v[lutIndex(int(im.Pix[i+2]), int(im.Pix[i+1]), int(im.Pix[i]))]
		}
	}
	return out
}
