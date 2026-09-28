package bindings

import "github.com/tolgahan/ed-sense/internal/dualsense"

// Detector turns controller and keyboard input into fired actions the way
// Elite does: a combo fires when its key is pressed with its modifiers held;
// a plain binding on a key that is also a modifier fires on release, and
// only if the key took part in no combo while held.
type Detector struct {
	b        *Bindings
	prevPad  dualsense.Button
	prevKeys map[int]bool
	comboed  map[input]bool // modifiers used in a combo during the current hold
	headlook bool
	ship     bool // the ship controls are live: a head look toggle counts
}

func NewDetector(b *Bindings) *Detector {
	return &Detector{b: b, prevKeys: map[int]bool{}, comboed: map[input]bool{}}
}

// Update returns the actions fired since the last call. keyDown reads a
// keyboard key (nil: the keyboard is not read).
func (d *Detector) Update(pad dualsense.State, keyDown func(vk int) bool) []Action {
	if d == nil || d.b == nil {
		return nil
	}
	in := frameInput{pad: pad, prevPad: d.prevPad, prevKeys: d.prevKeys, keyDown: keyDown}
	var fired []Action
	for _, a := range watched {
		for _, bd := range d.b.actions[a] {
			if d.fires(in, bd) {
				fired = append(fired, a)
				break
			}
		}
	}
	d.updateHeadlook(in)
	// any other button pressed while a modifier is held makes it a combo too
	// (Circle + d-pad bound to something not watched)
	for m := range d.b.modifiers {
		if in.held(m) && pad.Pressed&^m.button != 0 {
			d.comboed[m] = true
		}
		if !in.held(m) && !in.pressed(m) {
			delete(d.comboed, m)
		}
	}
	d.remember(in)
	return fired
}

// Headlook reports whether mouse headlook has the mouse: the head look
// binding held, or toggled on.
func (d *Detector) Headlook() bool { return d != nil && d.headlook }

// SetShipControls: whether the ship controls are live (in the ship, no
// panel open). Elsewhere the same buttons do other things (sprint on foot,
// the FSS), so they toggle no head look.
func (d *Detector) SetShipControls(on bool) {
	if d != nil {
		d.ship = on
	}
}

// ResetHeadlook turns head look off where presses can't be followed: out of
// the ship, in the main menu, EDSense paused.
func (d *Detector) ResetHeadlook() {
	if d != nil {
		d.headlook = false
	}
}

func (d *Detector) updateHeadlook(in frameInput) {
	if !d.b.Mouse.Headlook {
		d.headlook = false
		return
	}
	held := false
	for _, bd := range d.b.headlook {
		if !allHeld(in, bd.modifiers) {
			continue
		}
		if d.b.headlookToggles && in.pressed(bd.input) {
			if d.ship {
				d.headlook = !d.headlook
			}
			return
		}
		held = held || in.held(bd.input)
	}
	if !d.b.headlookToggles {
		d.headlook = held
	}
}

func allHeld(in frameInput, inputs []input) bool {
	for _, i := range inputs {
		if !in.held(i) {
			return false
		}
	}
	return true
}

func (d *Detector) fires(in frameInput, bd binding) bool {
	switch {
	case len(bd.modifiers) > 0:
		for _, m := range bd.modifiers {
			if !in.held(m) {
				return false
			}
		}
		if !in.pressed(bd.input) {
			return false
		}
		for _, m := range bd.modifiers {
			d.comboed[m] = true
		}
		return true
	case d.b.modifiers[bd.input]:
		return in.released(bd.input) && !d.comboed[bd.input]
	default:
		return in.pressed(bd.input) && !d.anyModifierHeld(in, bd.input)
	}
}

func (d *Detector) anyModifierHeld(in frameInput, except input) bool {
	for m := range d.b.modifiers {
		if m != except && in.held(m) {
			return true
		}
	}
	return false
}

func (d *Detector) remember(in frameInput) {
	d.prevPad = in.pad.Buttons
	for m := range d.b.modifiers {
		if m.key != 0 {
			d.prevKeys[m.key] = in.held(m)
		}
	}
	for _, a := range watched {
		for _, bd := range d.b.actions[a] {
			if bd.input.key != 0 {
				d.prevKeys[bd.input.key] = in.held(bd.input)
			}
		}
	}
	for _, bd := range d.b.headlook {
		if bd.input.key != 0 {
			d.prevKeys[bd.input.key] = in.held(bd.input)
		}
	}
}

// frameInput answers held / pressed / released for one update. A short tap
// between two updates shows up in pad.Pressed only, and counts as both
// pressed and released.
type frameInput struct {
	pad      dualsense.State
	prevPad  dualsense.Button
	prevKeys map[int]bool
	keyDown  func(vk int) bool
}

func (f frameInput) held(i input) bool {
	if i.button != 0 {
		return f.pad.Held(i.button)
	}
	return f.keyDown != nil && f.keyDown(i.key)
}

func (f frameInput) wasHeld(i input) bool {
	if i.button != 0 {
		return f.prevPad&i.button != 0
	}
	return f.prevKeys[i.key]
}

func (f frameInput) pressed(i input) bool {
	if i.button != 0 && f.pad.WasPressed(i.button) {
		return true
	}
	return f.held(i) && !f.wasHeld(i)
}

func (f frameInput) released(i input) bool {
	if i.button != 0 {
		return (f.wasHeld(i) || f.pad.WasPressed(i.button)) && !f.held(i)
	}
	return f.wasHeld(i) && !f.held(i)
}
