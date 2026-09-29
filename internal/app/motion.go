package app

import (
	"log"
	"time"
)

// motionOff decides whether DSX's motion output (gyro aim) is switched off:
// while Elite runs with gyro aim turned off in the settings, and in menus.
// Otherwise the DSX profile decides.
func motionOff(running, online, paused, gyroAim, inMenu bool) bool {
	return running && online && !paused && (!gyroAim || inMenu)
}

// applyMotion tells DSX, and repeats it every 3 s and for new controllers
// while the motion is off.
func (s *session) applyMotion(now time.Time, off, controllersChanged bool) {
	switch {
	case off != s.motionOff:
		s.out.SetMotionOff(s.controllers, off)
		switch {
		case !off:
			log.Print("Gyro back to the DSX profile")
		case s.cfg.GyroAim:
			log.Print("Gyro off (menu)")
		default:
			log.Print("Gyro off (gyro aim is off in the settings)")
		}
		s.motionOff, s.lastMotion = off, now
	case off && (controllersChanged || now.Sub(s.lastMotion) > 3*time.Second):
		s.out.SetMotionOff(s.controllers, true)
		s.lastMotion = now
	}
}
