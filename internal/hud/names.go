package hud

import (
	"math"
	"sort"

	"github.com/tolgahan/ed-sense/internal/elite"
)

// Letter widths of the HUD font in glyph heights, measured on 4K captures
// (letters not seen there are estimated).
var letterWidths = map[rune]float64{
	'A': 0.92, 'B': 0.84, 'C': 0.85, 'D': 0.85, 'E': 0.72, 'F': 0.70, 'G': 0.88, 'H': 0.85, 'I': 0.18,
	'J': 0.70, 'K': 0.82, 'L': 0.67, 'M': 1.15, 'N': 0.93, 'O': 0.95, 'P': 0.80, 'Q': 0.95, 'R': 0.82,
	'S': 0.77, 'T': 0.78, 'U': 0.85, 'V': 0.90, 'W': 1.25, 'X': 0.88, 'Y': 0.82, 'Z': 0.80,
	'-': 0.35, '.': 0.2, '\'': 0.2, '&': 0.9, '/': 0.6,
	'0': 0.75, '1': 0.45, '2': 0.75, '3': 0.75, '4': 0.78, '5': 0.75, '6': 0.75, '7': 0.72, '8': 0.75, '9': 0.75,
}

const (
	defaultLetterWidth = 0.85
	letterGap          = 0.08
	wordGap            = 0.55
	iconWidth          = 1.68 // a weapon's mount icon and the gap after it
)

// textWidth predicts the width of s in glyph heights.
func textWidth(s string) float64 {
	var w float64
	prev := ' '
	for _, c := range s {
		if c == ' ' {
			w += wordGap
		} else {
			if prev != ' ' {
				w += letterGap
			}
			if lw, ok := letterWidths[c]; ok {
				w += lw
			} else {
				w += defaultLetterWidth
			}
		}
		prev = c
	}
	return w
}

// entryWidth predicts how wide a module's entry is.
func entryWidth(m elite.Module) float64 {
	w := textWidth(m.Name)
	if !m.Utility {
		w += iconWidth
	}
	return w
}

// MatchEntry finds the module an entry shows, with how far off it is; -1
// when unsure.
func MatchEntry(e Entry, mods []elite.Module) (int, float64) {
	type candidate struct {
		i    int
		cost float64
	}
	var cands []candidate
	seen := map[string]bool{}
	for i, m := range mods {
		if seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		cost := math.Abs(math.Log(e.Width / entryWidth(m)))
		if e.Icon == m.Utility {
			// the icon is a few pixels at 1080p: less sure there
			if e.G >= 14 {
				cost += 0.1
			} else {
				cost += 0.05
			}
		}
		if e.Sub == AmmoLine && m.AmmoText != "" {
			// "77/1590" is much wider than "1/2"
			d := math.Abs(math.Log(e.SubWidth / textWidth(m.AmmoText)))
			cost += 0.25 * math.Max(0, d-0.2)
		}
		if e.Sub == AmmoLine && !m.Ammo {
			cost += 0.15
		}
		if e.Sub == NoSubLine && m.Ammo && !e.Red {
			cost += 0.03
		}
		cands = append(cands, candidate{i, cost})
	}
	if len(cands) == 0 {
		return -1, 0
	}
	sort.Slice(cands, func(a, b int) bool { return cands[a].cost < cands[b].cost })
	best := cands[0]
	if best.cost > 0.13 {
		return -1, best.cost
	}
	if len(cands) > 1 && cands[1].cost-best.cost < 0.03 && mods[cands[1].i].Class != mods[best.i].Class {
		return -1, best.cost
	}
	return best.i, best.cost
}
