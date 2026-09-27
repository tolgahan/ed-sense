package hud

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// Palette holds the on-screen colours of what the reader looks for.
type Palette struct {
	Shield  vision.Color // shield % and the ship hologram
	Heat    vision.Color // heat %
	Flame   vision.Color // the heat icon, and out-of-range weapons
	Flash   vision.Color // hit flashes on the hologram
	Main    vision.Color // the HUD's main colour: hull %, capacitors, weapon lists
	NoFlash bool         // flash colour unknown or too close to the shield colour
}

// DefaultPalette is the standard HUD, measured from recorded gameplay.
var DefaultPalette = Palette{
	Shield: vision.Color{41, 200, 207},
	Heat:   vision.Color{186, 108, 22},
	Flame:  vision.Color{147, 30, 27},
	Flash:  vision.Color{56, 98, 193},
	Main:   vision.Color{214, 135, 12},
}

// How close a pixel must be to each colour.
var (
	matchShield      = vision.Match{HueWidth: 25, MinSat: 0.25, MinVal: 0.3, ChromaTol: 0.5}
	matchHeat        = vision.Match{HueWidth: 18, MinSat: 0.4, MinVal: 0.4, ChromaTol: 0.4}
	matchFlame       = vision.Match{HueWidth: 14, MinSat: 0.67, MinVal: 0.78, ChromaTol: 0.35}
	matchFlash       = vision.Match{HueWidth: 22, MinSat: 0.63, MinVal: 0.66, ChromaTol: 0.35}
	matchCalibration = vision.Match{MinVal: 0.3, ChromaTol: 0.5, ByChroma: true}
)

// underMatrix predicts the palette under Elite's HUD colour matrix:
// out = R*MatrixRed + G*MatrixGreen + B*MatrixBlue.
func (p Palette) underMatrix(m [3][3]float64) Palette {
	tf := func(c vision.Color) vision.Color {
		var out vision.Color
		for k := range 3 {
			v := float64(c[0])/255*m[0][k] + float64(c[1])/255*m[1][k] + float64(c[2])/255*m[2][k]
			out[k] = uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
		}
		return out
	}
	return Palette{Shield: tf(p.Shield), Heat: tf(p.Heat), Flame: tf(p.Flame), Flash: tf(p.Flash), Main: tf(p.Main)}
}

// withFlashCheck turns hit-flash detection off when the flash colour is too
// close to the shield text and rings: they would look like constant hits.
func (p Palette) withFlashCheck() Palette {
	if vision.ColorDistance(p.Shield, p.Flash) < 0.35 {
		p.NoFlash = true
	}
	return p
}

// WithCalibration is the palette with the colours calibration found.
func (p Palette) WithCalibration(c Calibration) Palette {
	p.Shield, p.Heat, p.Main, p.NoFlash = c.Shield, c.Heat, c.Main, false
	return p.withFlashCheck()
}

// The palette comes from, in order of priority: the player's hud_colors,
// colours learned from the screen (hud_palette.json), the prediction from
// the colour matrix, the standard colours.

const learnedFile = "hud_palette.json"

// learnedPalette is what calibration found. It belongs to one colour
// matrix; another matrix means learning again.
type learnedPalette struct {
	Matrix string `json:"matrix"`
	Shield string `json:"shield"`
	Heat   string `json:"heat"`
	Main   string `json:"hull,omitempty"`
}

// ResolvePalette returns the palette to read with, where it came from (for
// the log), and the key of the current colour matrix (to save what is learned).
func ResolvePalette(manual map[string]string, dataDir string) (pal Palette, source, matrixKey string) {
	m, ok := colourMatrix(elite.GraphicsOverrideFile())
	matrixKey = "none"
	if ok {
		matrixKey = fmt.Sprintf("%v", m)
	}
	pal, source = DefaultPalette, "standard HUD colours"
	if ok && !isIdentity(m) {
		pal, source = DefaultPalette.underMatrix(m), "your HUD colour matrix (GraphicsConfigurationOverride.xml)"
	}
	if lp, hasMain, ok := readLearned(dataDir, matrixKey); ok {
		pal.Shield, pal.Heat, source = lp.Shield, lp.Heat, "colours learned from your screen ("+learnedFile+")"
		if hasMain {
			pal.Main = lp.Main
		}
	}
	noFlash, set := applyManual(&pal, manual)
	if set {
		source += " + your hud_colors"
	}
	pal = pal.withFlashCheck()
	pal.NoFlash = pal.NoFlash || noFlash
	return pal, source, matrixKey
}

// readLearned returns the learned colours for this colour matrix; the main
// colour was not learned by older versions.
func readLearned(dataDir, matrixKey string) (p Palette, hasMain, ok bool) {
	b, err := os.ReadFile(filepath.Join(dataDir, learnedFile))
	if err != nil {
		return p, false, false
	}
	var lp learnedPalette
	if json.Unmarshal(b, &lp) != nil || lp.Matrix != matrixKey {
		return p, false, false
	}
	shield, shieldOK := vision.ParseHex(lp.Shield)
	heat, heatOK := vision.ParseHex(lp.Heat)
	if !shieldOK || !heatOK {
		return p, false, false
	}
	p.Shield, p.Heat = shield, heat
	p.Main, hasMain = vision.ParseHex(lp.Main)
	return p, hasMain, true
}

// SaveLearned keeps colours found on screen for the next start.
func SaveLearned(dataDir, matrixKey string, p Palette) error {
	b, err := json.MarshalIndent(learnedPalette{Matrix: matrixKey, Shield: p.Shield.Hex(), Heat: p.Heat.Hex(), Main: p.Main.Hex()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, learnedFile), b, 0o644)
}

// applyManual sets the player's hud_colors. It returns whether flashes were
// turned off, and whether any colour was set.
func applyManual(p *Palette, manual map[string]string) (noFlash, set bool) {
	for k, v := range manual {
		k = strings.ToLower(k)
		if k == "flash" && strings.EqualFold(strings.TrimSpace(v), "off") {
			noFlash, set = true, true
			continue
		}
		c, ok := vision.ParseHex(v)
		if !ok {
			continue
		}
		switch k {
		case "shield":
			p.Shield = c
		case "heat":
			p.Heat = c
		case "flame":
			p.Flame = c
		case "flash":
			p.Flash = c
		case "hull", "main":
			p.Main = c
		default:
			continue
		}
		set = true
	}
	return noFlash, set
}

// ManualColours reports whether the player set the shield or heat colour,
// in which case colours are not learned from the screen.
func ManualColours(manual map[string]string) bool {
	for k, v := range manual {
		if k = strings.ToLower(k); (k == "shield" || k == "heat") && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

var (
	xmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	matrixRows = []*regexp.Regexp{
		regexp.MustCompile(`<MatrixRed>([^<]*)</MatrixRed>`),
		regexp.MustCompile(`<MatrixGreen>([^<]*)</MatrixGreen>`),
		regexp.MustCompile(`<MatrixBlue>([^<]*)</MatrixBlue>`),
	}
)

// colourMatrix reads the HUD colour matrix from GraphicsConfigurationOverride.xml.
// Players often keep other presets commented out, so comments are dropped.
func colourMatrix(path string) ([3][3]float64, bool) {
	var m [3][3]float64
	b, err := os.ReadFile(path)
	if err != nil {
		return m, false
	}
	s := xmlComment.ReplaceAllString(string(b), "")
	start := strings.Index(s, "<GUIColour>")
	if start < 0 {
		return m, false
	}
	s = s[start:]
	if end := strings.Index(s, "</GUIColour>"); end > 0 {
		s = s[:end]
	}
	for i, re := range matrixRows {
		row := re.FindStringSubmatch(s)
		if row == nil {
			return m, false
		}
		parts := strings.Split(row[1], ",")
		if len(parts) != 3 {
			return m, false
		}
		for k, p := range parts {
			v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil {
				return m, false
			}
			m[i][k] = v
		}
	}
	return m, true
}

func isIdentity(m [3][3]float64) bool {
	for i := range 3 {
		for k := range 3 {
			want := 0.0
			if i == k {
				want = 1
			}
			if math.Abs(m[i][k]-want) > 1e-6 {
				return false
			}
		}
	}
	return true
}
