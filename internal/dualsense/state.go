// Package dualsense talks to DSX's virtual DualSense: it reads the
// controller's input reports and writes rumble.
package dualsense

type Button uint32

const (
	Square Button = 1 << iota
	Cross
	Circle
	Triangle
	L1
	R1
	L2
	R2
	Create
	Options
	L3
	R3
	PS
	Touchpad
	Mute
	DpadUp
	DpadRight
	DpadDown
	DpadLeft
)

// State is the controller input the app uses.
type State struct {
	OK      bool
	L2, R2  uint8  // analog, 0-255
	Buttons Button // held now
	Pressed Button // pressed since the previous read, so short taps are not missed
}

func (s State) Held(b Button) bool       { return s.Buttons&b != 0 }
func (s State) WasPressed(b Button) bool { return s.Pressed&b != 0 }

// TriggerThreshold is how far an analog trigger must be pulled to count as held.
const TriggerThreshold = 40

func (s State) R2Held() bool { return s.R2 > TriggerThreshold }
func (s State) L2Held() bool { return s.L2 > TriggerThreshold }

// ParseInputReport reads a USB (0x01) or Bluetooth (0x31) input report.
func ParseInputReport(b []byte) (State, bool) {
	var base int
	switch {
	case len(b) >= 11 && b[0] == 0x01:
		base = 1
	case len(b) >= 12 && b[0] == 0x31:
		base = 2
	default:
		return State{}, false
	}
	st := State{OK: true, L2: b[base+4], R2: b[base+5]}
	st.Buttons = dpad(b[base+7]&0x0F) | buttons(b[base+7], b[base+8], b[base+9])
	return st, true
}

// dpad decodes the hat: 0 = up, clockwise, 8 = released.
func dpad(hat byte) Button {
	return [...]Button{
		DpadUp, DpadUp | DpadRight, DpadRight, DpadDown | DpadRight,
		DpadDown, DpadDown | DpadLeft, DpadLeft, DpadUp | DpadLeft,
		0, 0, 0, 0, 0, 0, 0, 0,
	}[hat]
}

func buttons(b0, b1, b2 byte) Button {
	bits := []struct {
		set bool
		b   Button
	}{
		{b0&0x10 != 0, Square}, {b0&0x20 != 0, Cross}, {b0&0x40 != 0, Circle}, {b0&0x80 != 0, Triangle},
		{b1&0x01 != 0, L1}, {b1&0x02 != 0, R1}, {b1&0x04 != 0, L2}, {b1&0x08 != 0, R2},
		{b1&0x10 != 0, Create}, {b1&0x20 != 0, Options}, {b1&0x40 != 0, L3}, {b1&0x80 != 0, R3},
		{b2&0x01 != 0, PS}, {b2&0x02 != 0, Touchpad}, {b2&0x04 != 0, Mute},
	}
	var out Button
	for _, x := range bits {
		if x.set {
			out |= x.b
		}
	}
	return out
}
