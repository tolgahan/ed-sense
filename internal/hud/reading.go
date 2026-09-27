// Package hud reads Elite Dangerous' cockpit HUD from screen captures: the
// shield, hull and heat %, hits on the ship and on the target, the
// capacitors, the throttle's blue zone and the fire group lists.
//
// Positions vary: the cockpit sways, and resolution, field of
// view and ship differ between players. Text is found by colour and shape,
// straightened and matched against glyph templates (see package vision).
package hud

import "github.com/tolgahan/ed-sense/internal/hud/vision"

// Number is a "NN%" read on screen.
type Number struct {
	Text  string      // as read, e.g. "85%"; empty when not found
	Score float64     // mean glyph correlation, up to 1
	Box   vision.Rect // image pixels
	GH    float64     // glyph height
}

func (n Number) Found() bool { return n.Text != "" }

func (n Number) Value() (int, bool) { return percentValue(n.Text) }

func (n Number) Center() (x, y float64) {
	return float64(n.Box.X0) + float64(n.Box.W())/2, float64(n.Box.Y0) + float64(n.Box.H())/2
}

// Target is the target panel, left of the radar.
type Target struct {
	Shield, Hull Number
	ShieldsDown  bool    // only the hull % was found
	X, Y, GH     float64 // where the shield % is, or would be
	Splash       float64 // hit flashes on the target's hologram, area / GH^2
}

// Found reports whether the panel was seen.
func (t Target) Found() bool { return t.Shield.Found() || t.Hull.Found() }

// The fire group lists on the cockpit struts.
const (
	Secondary = 0 // left, L2
	Primary   = 1 // right, R2
)

// Capacitor indexes.
const (
	SYS = iota
	ENG
	WEP
)

// Reading is what one frame shows. Coordinates are image pixels.
type Reading struct {
	Shield    Number  // under the ship hologram on the right-hand panel
	Splash    float64 // hit flashes on the ship hologram, area / GH^2
	SplashPan float64 // where they are: -1 left of the ship .. +1 right
	Heat      Number  // next to the flame icon at the top left of the radar
	Hull      Number  // below left of the shield %
	Target    Target

	BlueZoneSeen bool // the throttle's blue zone is in view
	InBlueZone   bool // ... and the throttle is in it

	CapsOK bool
	Caps   [3]float64 // capacitor charge 0-1: SYS, ENG, WEP

	Lists [2]ListRead // Secondary, Primary
}

// Where to look, as parts of the game window.

func shieldArea(w, h int) vision.Rect {
	return vision.Rect{X0: int(float64(w) * 0.5), Y0: int(float64(h) * 0.5), X1: int(float64(w) * 0.97), Y1: int(float64(h) * 0.97)}
}

func heatArea(w, h int) vision.Rect {
	return vision.Rect{X0: int(float64(w) * 0.25), Y0: int(float64(h) * 0.5), X1: int(float64(w) * 0.65), Y1: int(float64(h) * 0.95)}
}

// targetArea ends left of the heat %, which is the same orange "NN%" as a
// target's hull %.
func targetArea(w, h int) vision.Rect {
	return vision.Rect{X0: int(float64(w) * 0.03), Y0: int(float64(h) * 0.5), X1: int(float64(w) * 0.42), Y1: int(float64(h) * 0.97)}
}

// listArea: the lists sit at a fixed angle from the view's centre, so the
// area is placed in screen heights from the centre (16:9 and ultrawide alike).
func listArea(w, h int, right bool) vision.Rect {
	cx, H := float64(w)/2, float64(h)
	if right {
		return vision.Rect{X0: int(cx + 0.09*H), Y0: int(0.3 * H), X1: int(cx + 0.68*H), Y1: int(0.8 * H)}.Clip(w, h)
	}
	return vision.Rect{X0: int(cx - 0.68*H), Y0: int(0.3 * H), X1: int(cx - 0.09*H), Y1: int(0.8 * H)}.Clip(w, h)
}

// Read reads the shield and heat % from a whole game window.
func Read(im *vision.Image, pal Palette) Reading {
	var r Reading
	r.Shield, r.Splash, r.SplashPan = readShield(im, shieldArea(im.W, im.H), im.H, pal)
	r.Heat = readHeat(im, heatArea(im.W, im.H), im.H, pal)
	return r
}

// ReadAll reads everything the HUD reader knows from a whole game window.
func ReadAll(im *vision.Image, pal Palette) Reading {
	r := Read(im, pal)
	if r.Shield.Found() {
		x, y := r.Shield.Center()
		r.Hull = readHull(im, x, y, r.Shield.GH, im.H, pal)
		r.Caps, r.CapsOK = readCapacitors(im, x, y, r.Shield.GH, pal)
	}
	if r.Heat.Found() {
		x, y := r.Heat.Center()
		r.BlueZoneSeen, r.InBlueZone = readBlueZone(im, x, y, r.Heat.GH)
	}
	r.Target = readTarget(im, targetArea(im.W, im.H), im.H, pal, nil)
	for side, right := range []bool{false, true} {
		r.Lists[side] = readList(im, listArea(im.W, im.H, right), im.H, pal, right)
	}
	return r
}
