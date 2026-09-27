package hud

import "github.com/tolgahan/ed-sense/internal/hud/vision"

// readShield finds and reads the shield % in r, and the hit flashes on the
// ship hologram above it.
func readShield(im *vision.Image, r vision.Rect, screenH int, pal Palette) (shield Number, splash, pan float64) {
	r = r.Clip(im.W, im.H)
	if r.W() < 8 || r.H() < 8 {
		return Number{}, 0, 0
	}
	g := vision.Score(im, r, pal.Shield, matchShield)
	labels, lines := vision.TextLines(g, screenH, textThreshold)
	for _, line := range lines {
		if n, ok := readLine(g, labels, line, shieldFont, r.X0, r.Y0); ok && (!shield.Found() || n.Score > shield.Score) {
			shield = n
		}
	}
	if !shield.Found() || pal.NoFlash {
		return shield, 0, 0
	}
	x, y := shield.Center()
	splash, pan = splashAbove(im, x, y, shield.GH, r, pal)
	return shield, splash, pan
}

// splashAbove measures the hit flashes on the hologram above a shield text at
// (x, y), inside r: their area / gh^2, and where they are, -1 (left) .. +1 (right).
func splashAbove(im *vision.Image, x, y, gh float64, r vision.Rect, pal Palette) (area, pan float64) {
	a := vision.Rect{X0: int(x - 11*gh), Y0: int(y - 11*gh), X1: int(x + 11*gh), Y1: int(y + 0.5*gh)}.Intersect(r).Clip(im.W, im.H)
	if a.W() <= 0 || a.H() <= 0 {
		return 0, 0
	}
	mask := vision.Mask(im, a, pal.Flash, matchFlash)
	n, sumX := 0, 0
	for yy := range a.H() {
		for xx := range a.W() {
			if mask[yy*a.W()+xx] {
				n++
				sumX += xx + a.X0
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	return float64(n) / (gh * gh), (float64(sumX)/float64(n) - x) / (6 * gh)
}
