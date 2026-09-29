// Package dualsense talks to DSX's virtual DualSense: it reads the
// controller's input reports, writes rumble, and streams native haptics to
// the controller's audio device.
package dualsense

import (
	"encoding/binary"
	"math"
)

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
	L2, R2  uint8      // analog, 0-255
	Buttons Button     // held now
	Pressed Button     // pressed since the previous read, so short taps are not missed
	Gyro    [3]int16   // raw angular velocity
	Accel   [3]int16   // raw acceleration, same axes as Gyro
	Clock   uint32     // the sensor clock, 3 MHz, wraps at 2^32; DSX copies the controller's
	Sticks  [4]float64 // LX, LY, RX, RY in -1..1
	Touch   bool       // a finger rests on the touchpad
}

func (s State) Held(b Button) bool       { return s.Buttons&b != 0 }
func (s State) WasPressed(b Button) bool { return s.Pressed&b != 0 }

// TriggerThreshold is how far an analog trigger must be pulled to count as held.
const TriggerThreshold = 40

func (s State) R2Held() bool { return s.R2 > TriggerThreshold }
func (s State) L2Held() bool { return s.L2 > TriggerThreshold }

// GyroDegPerSec is how fast the controller turns about one axis: 0 pitch,
// 1 yaw, 2 roll.
func (s State) GyroDegPerSec(axis int) float64 { return float64(s.Gyro[axis]) / 16.4 }

// AimDegPerSec is how fast the controller pitches and yaws, the rotation
// gyro aim turns into mouse movement; rolling it aims nothing.
func (s State) AimDegPerSec() float64 {
	pitch, yaw := float64(s.Gyro[0]), float64(s.Gyro[1])
	return math.Hypot(pitch, yaw) / 16.4 // 16.4 LSB per deg/s
}

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
	for i := range st.Sticks {
		st.Sticks[i] = (float64(b[base+i]) - 127.5) / 127.5
	}
	if len(b) >= base+21 {
		for i := range st.Gyro {
			st.Gyro[i] = int16(uint16(b[base+15+2*i]) | uint16(b[base+16+2*i])<<8)
		}
	}
	if len(b) >= base+31 {
		for i := range st.Accel {
			st.Accel[i] = int16(uint16(b[base+21+2*i]) | uint16(b[base+22+2*i])<<8)
		}
		st.Clock = binary.LittleEndian.Uint32(b[base+27:])
	}
	// Finger 1: a contact byte (bit 7 set: not touching), then 12-bit x / y.
	// All-zero touch data (seen on the virtual pad) is no touch.
	if len(b) >= base+36 {
		contact := b[base+32]
		x := uint16(b[base+33]) | uint16(b[base+34]&0x0F)<<8
		y := uint16(b[base+34]>>4) | uint16(b[base+35])<<4
		st.Touch = contact&0x80 == 0 && (x != 0 || y != 0)
	}
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
