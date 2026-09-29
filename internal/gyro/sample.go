package gyro

import "time"

// Sample is one motion report in the controller's axes, PlayStation style
// with Y up: X pitch, Y yaw, Z roll.
type Sample struct {
	Gyro    [3]float64 // deg/s
	Accel   [3]float64 // g; all zero when the backend has no accelerometer
	Stamp   uint32     // the controller's sensor clock, wrapping at 2^32
	StampHz float64    // its ticks per second; 0: no sensor clock
	At      time.Time  // when EDSense read the report
	Touch   bool       // a finger rests on the touchpad
}

// empty: the report carries no motion at all. DSX sends such reports while
// it pauses the gyro for a touch, with its motion DISABLED, and when it
// passes no motion on.
func (s Sample) empty() bool { return s.Gyro == [3]float64{} && s.Accel == [3]float64{} }

// Mouse moves the mouse by relative counts, +x right, +y down. False:
// Windows refused.
type Mouse interface{ Move(dx, dy int32) bool }

// MouseFunc lets a plain function be a Mouse.
type MouseFunc func(dx, dy int32) bool

func (f MouseFunc) Move(dx, dy int32) bool { return f(dx, dy) }
