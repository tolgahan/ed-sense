package hud

import (
	"math"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// Where the hull % sits relative to the shield % text, in glyph heights.
// Measured on 1440p and 4K captures; the HUD sways, so the box is loose.
var (
	hullBox      = box{-12, 1.5, -3.5, 9}
	hullX, hullY = -7.0, 4.8 // usual centre
)

// readHull reads the hull % next to the shield % at (x, y).
func readHull(im *vision.Image, x, y, gh float64, screenH int, pal Palette) Number {
	var in []Number
	for _, n := range numbers(im, hullBox.around(x, y, gh, 3, 1.5), screenH, pal.Main, matchHeat, 0.68) {
		cx, cy := n.Center()
		if hullBox.contains((cx-x)/gh, (cy-y)/gh) {
			in = append(in, n)
		}
	}
	hull, _ := bestNear(in, x+hullX*gh, y+hullY*gh, 4*gh)
	return hull
}

// The target panel mirrors the ship panel: the target's shield % in the
// shield colour under its hologram, its hull % in the main colour below
// right. When the target's shields are down only the hull % is left.
var (
	targetHullBox            = box{3.5, 1.5, 12, 9} // relative to the shield %
	targetHullX, targetHullY = 7.0, 4.8
)

// readTarget finds the target panel in r: a shield-coloured "NN%" with the
// hull % below right of it, or the hull % alone. last is where the panel was
// found before (nil when searching); a lone hull % must then be where it
// belongs, and may be fainter.
func readTarget(im *vision.Image, r vision.Rect, screenH int, pal Palette, last *Target) Target {
	var t Target
	bestScore := -1.0
	for _, s := range numbers(im, r, screenH, pal.Shield, matchShield, 0.45) {
		sx, sy := s.Center()
		var in []Number
		// the hull % is nearer the viewer than the shield %: as big or bigger
		for _, n := range numbers(im, targetHullBox.around(sx, sy, s.GH, 3, 1.5), screenH, pal.Main, matchHeat, 0.55) {
			cx, cy := n.Center()
			if targetHullBox.contains((cx-sx)/s.GH, (cy-sy)/s.GH) && n.GH >= 0.9*s.GH {
				in = append(in, n)
			}
		}
		hull, ok := bestNear(in, sx+targetHullX*s.GH, sy+targetHullY*s.GH, 4*s.GH)
		if !ok && s.Score < 0.75 {
			continue // a lone shield-coloured number must be clear
		}
		score := s.Score
		if ok {
			score += hull.Score
		}
		if score > bestScore {
			bestScore = score
			t = Target{Shield: s, Hull: hull, X: sx, Y: sy, GH: s.GH}
		}
	}
	if bestScore <= 0 {
		var ok bool
		if t, ok = loneTargetHull(im, r, screenH, pal, last); !ok {
			return Target{}
		}
	}
	// hits on the target's shields flash on its hologram, as on ours
	if !pal.NoFlash {
		t.Splash, _ = splashAbove(im, t.X, t.Y, t.GH, r, pal)
	}
	return t
}

// loneTargetHull: a hull % alone, the target's shields being down.
func loneTargetHull(im *vision.Image, r vision.Rect, screenH int, pal Palette, last *Target) (Target, bool) {
	minScore := 0.78
	if last != nil {
		minScore = 0.55
	}
	hulls := numbers(im, r, screenH, pal.Main, matchHeat, minScore)
	var ex, ey, eg float64
	if last != nil {
		ex, ey, eg = last.X, last.Y, last.GH
		in := hulls[:0]
		for _, n := range hulls {
			cx, cy := n.Center()
			if targetHullBox.contains((cx-ex)/eg, (cy-ey)/eg) {
				in = append(in, n)
			}
		}
		hulls = in
	}
	hull, ok := bestNear(hulls, ex+targetHullX*eg, ey+targetHullY*eg, 4*math.Max(eg, 1))
	if !ok {
		return Target{}, false
	}
	t := Target{Hull: hull, ShieldsDown: true, X: ex, Y: ey, GH: eg} // the panel stays where it was
	if last == nil {
		cx, cy := hull.Center()
		t.X, t.Y, t.GH = cx-targetHullX*hull.GH, cy-targetHullY*hull.GH, hull.GH
	}
	return t, true
}
