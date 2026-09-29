package app

import (
	"log"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
)

// dsxMotion is what EDSense has DSX's motion page do. The zero value is the
// profile's own, which is also where DSX starts.
type dsxMotion int

const (
	motionProfile  dsxMotion = iota // the DSX profile's gyro aim
	motionDisabled                  // no gyro aim: a menu, or gyro aim off
	motionNone                      // no mouse from DSX: EDSense's gyro aims
)

func (m dsxMotion) mode() backend.MotionMode {
	switch m {
	case motionDisabled:
		return backend.MotionDisabled
	case motionNone:
		return backend.MotionNone
	}
	return backend.MotionProfile
}

// motionPolicy decides what DSX's motion page does. With Elite closed or
// DSX not answering, the profile decides. While EDSense calibrates the gyro
// (asked for from the tray, so also while paused) and with EDSense's own
// gyro (own), DSX only passes the motion on, in menus too, since EDSense
// stops aiming there itself. Paused, the profile decides. Otherwise DSX's
// gyro is off in menus and with gyro aim off.
func motionPolicy(running, online, paused, gyroAim, inMenu, own, calibrating bool) dsxMotion {
	switch {
	case !running || !online:
		return motionProfile
	case calibrating:
		return motionNone
	case paused:
		return motionProfile
	case own:
		return motionNone
	case !gyroAim || inMenu:
		return motionDisabled
	}
	return motionProfile
}

// applyMotion tells DSX, and repeats it every 3 s and for new controllers
// while it is not the profile's. With EDSense's gyro it is also repeated
// when a finger leaves the touchpad, in case DSX's touch binding puts the
// profile's motion to mouse back. With no controller listed nothing is
// told, so the gyro waits until DSX has one to switch.
func (s *session) applyMotion(now time.Time, want dsxMotion, own, controllersChanged, touchLifted bool) {
	if len(s.controllers) == 0 && want != motionProfile {
		return
	}
	switch {
	case want != s.motion:
		s.out.SetMotion(s.controllers, want.mode())
		switch {
		case want == motionProfile:
			log.Print("Gyro back to the DSX profile")
		case want == motionNone && own:
			log.Print("Gyro: EDSense aims, DSX's motion to mouse is off")
		case want == motionNone:
			log.Print("Gyro: DSX's motion to mouse is off while EDSense calibrates")
		case s.cfg.GyroAim:
			log.Print("Gyro off (menu)")
		default:
			log.Print("Gyro off (gyro aim is off in the settings)")
		}
		if want == motionNone {
			s.noneSince = now
		}
		s.motion, s.lastMotion = want, now
	case want != motionProfile && (controllersChanged || now.Sub(s.lastMotion) > 3*time.Second || want == motionNone && touchLifted):
		s.out.SetMotion(s.controllers, want.mode())
		if want == motionNone && controllersChanged {
			s.noneSince = now // a new controller had the profile's until now
		}
		s.lastMotion = now
	}
}
