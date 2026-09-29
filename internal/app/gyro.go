package app

import (
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// EDSense's own gyro aim. The gyro package turns every motion report into
// mouse movement on the backend's reading goroutine. The loop decides when
// it may aim, keeps DSX's motion to mouse off meanwhile, and leaves the aim
// to DSX's gyro when EDSense's cannot work: no motion data, or Elite running
// as administrator.

// gyroSettings: the settings file's gyro keys.
func gyroSettings(c *config.Config) gyro.Settings {
	return gyro.Settings{
		CountsPerDeg:  [2]float64{gyro.DSXCountsPerDegree * c.GyroSensitivityX, gyro.DSXCountsPerDegree * c.GyroSensitivityY},
		RollMix:       c.GyroRollMix,
		Exact:         c.GyroLowSpeed == config.GyroLowExact,
		AutoCalibrate: c.GyroAutoCalibrate,
	}
}

func (a *App) biasPath() string { return filepath.Join(a.dataDir(), gyro.BiasFile) }

// CalibrateGyro learns the gyro's drift from the next 2 s, with the
// controller lying still. It runs on the tray's goroutine, so it only asks
// the loop, which starts it and tells the result.
func (a *App) CalibrateGyro() {
	if a.gyro == nil {
		a.tell("This controller connection passes no gyro.")
		return
	}
	request(a.calibrateRequests)
}

// startCalibration: the loop takes a calibration asked for from the tray.
// Meanwhile DSX passes the motion on without moving the mouse, in menus
// and paused too (motionPolicy).
func (s *session) startCalibration() {
	if !s.running {
		s.tell("EDSense reads the controller while Elite runs. Start Elite, then try again.")
		return
	}
	s.pad.Maintain()
	if !s.pad.Available() {
		s.tell("EDSense cannot open DSX's virtual DualSense, so it cannot calibrate the gyro. Is the controller connected in DSX?")
		return
	}
	log.Print("Gyro: calibrating")
	s.gyro.Calibrate(2 * time.Second)
}

// wantGyro: the settings give the aim to EDSense's gyro, and the DSX
// profile's gyro is motion to mouse, or can't be read. EDSense's gyro
// replaces only that, never a gyro the player has on a stick or keys.
func (s *session) wantGyro() bool {
	if s.gyro == nil || s.cfg.GyroBy != config.GyroByEDSense {
		return false
	}
	mouse, known := s.profile.GyroToMouse()
	return mouse || !known
}

// noteProfile logs once when the DSX profile keeps the aim from EDSense's
// gyro.
func (s *session) noteProfile() {
	mouse, known := s.profile.GyroToMouse()
	keep := s.gyro != nil && s.cfg.GyroBy == config.GyroByEDSense && known && !mouse
	if keep && !s.saidProfile {
		log.Print("Gyro: the DSX profile for Elite does not use motion to mouse, so EDSense's gyro stays off and the profile's gyro works as set")
	}
	s.saidProfile = keep
}

// ownGyro: EDSense's gyro can aim now. The pad is asked only when wanted,
// so nothing changes with gyro_by "dsx".
func (s *session) ownGyro() bool {
	return s.wantGyro() && !s.gyroNoData && !s.eliteBlocked && s.pad.Available()
}

func (s *session) gyroStatus() gyro.Status {
	if s.gyro == nil {
		return gyro.Status{}
	}
	return s.gyro.Status()
}

// checkElevated: once per Elite start, whether Windows keeps EDSense's
// mouse movement from Elite (Elite running as administrator).
func (s *session) checkElevated() {
	if !s.running || !s.wantGyro() || s.blockChecked {
		return
	}
	s.blockChecked = true
	if s.eliteBlocked = s.blocked(); s.eliteBlocked {
		log.Print("Gyro: Elite runs as administrator, so EDSense's mouse movement cannot reach it; DSX's gyro aims")
		s.tell("Elite runs as administrator, so EDSense's gyro cannot reach it and DSX's gyro aims instead.\n\n" +
			"Start Elite (and Steam) normally, or run EDSense as administrator too.")
	}
}

// holdGyro sets why EDSense's gyro does not aim, or lets it aim, and logs
// the reason when it changes. Reports fed before the next tick see this
// hold, so leaving Elite or opening a menu costs at most one tick of motion.
func (s *session) holdGyro(now time.Time, own, online, paused, inMenu bool, st gyro.Status) {
	if s.gyro == nil {
		return
	}
	defer func() { s.seen = st }()
	s.tellManual(st)
	// DSX's own gyro may aim: its slow turns are no drift either
	s.gyro.SetDSXAims(s.running && s.motion == motionProfile)
	s.noteProfile()
	if !s.wantGyro() {
		s.gyro.SetHold(gyro.HoldOff)
		s.hold = gyro.HoldOff
		return
	}
	var h gyro.Hold
	if !own || !s.cfg.GyroAim {
		h |= gyro.HoldOff
	}
	if !s.running || own && !s.front() {
		h |= gyro.HoldElite
	}
	if paused {
		h |= gyro.HoldPaused
	}
	if inMenu {
		h |= gyro.HoldMenu
	}
	// DSX's mouse may be on until it is told NONE, which needs a
	// controller listed; named only when it is the one reason, since
	// pausing also hands DSX its profile back
	if h == 0 && (!online || s.motion != motionNone || len(s.controllers) == 0) {
		h |= gyro.HoldDSX
	}
	s.gyro.SetHold(h)
	if own && h != s.hold {
		if h == 0 {
			log.Print("EDSense gyro: aiming")
		} else {
			log.Printf("EDSense gyro: waiting (%s)", holdReason(h, s.cfg.GyroAim))
		}
	}
	s.hold = h
	s.checkOwnGyro(now, st)
}

// holdReason: the reasons in h, for the log.
func holdReason(h gyro.Hold, gyroAim bool) string {
	var r []string
	if h&gyro.HoldPaused != 0 {
		r = append(r, "paused")
	}
	if h&gyro.HoldElite != 0 {
		r = append(r, "Elite not running or not in front")
	}
	if h&gyro.HoldDSX != 0 {
		r = append(r, "waiting for DSX")
	}
	if h&gyro.HoldMenu != 0 {
		r = append(r, "menu")
	}
	if h&gyro.HoldOff != 0 && !gyroAim {
		r = append(r, "gyro aim is off")
	}
	return strings.Join(r, ", ")
}

// checkOwnGyro falls back to DSX's gyro when no motion arrives while DSX
// passes it on, and logs what the aim learns or cannot do.
func (s *session) checkOwnGyro(now time.Time, st gyro.Status) {
	if !s.gyroNoData && s.motion == motionNone && len(s.controllers) > 0 && now.Sub(s.noneSince) > time.Second && st.ZeroFor >= 2*time.Second {
		s.gyroNoData = true
		log.Print("Gyro: no motion data from DSX's virtual DualSense while its motion to mouse is off, so DSX's gyro aims")
		s.tell("EDSense's gyro gets no motion data from DSX's virtual DualSense, so DSX's own gyro aims for now.\n\n" +
			"Untick \"EDSense gyro\" in the tray to keep DSX's gyro. .\\EDSense.exe -gyrotest shows more.")
	}
	if st.Learned != s.seen.Learned {
		log.Printf("Gyro: calibrated (drift %.2f %.2f %.2f deg/s)", st.Bias[0], st.Bias[1], st.Bias[2])
	}
	if st.MoveFailed > 0 && !s.warnedMove {
		log.Print("Gyro: Windows refused a mouse movement (a locked screen, or a program running as administrator in front)")
		s.warnedMove = true
	}
}

// tellManual reports a calibration asked for from the tray, and saves it.
func (s *session) tellManual(st gyro.Status) {
	if st.Manual == s.seen.Manual {
		return
	}
	if st.ManualOK {
		log.Print("Gyro: calibrated by hand")
		if err := gyro.SaveBias(s.biasPath(), st.Bias); err != nil {
			log.Printf("Gyro: %v", err)
		}
		s.tell("Gyro calibrated.")
		return
	}
	if st.ManualNoData {
		log.Print("Gyro: calibration failed, no motion data came")
		s.tell("No motion data came from DSX's virtual DualSense, so EDSense could not calibrate the gyro.\n\n" +
			".\\EDSense.exe -gyrotest shows more.")
		return
	}
	log.Print("Gyro: calibration failed, the controller moved")
	s.tell("The controller moved while EDSense calibrated the gyro. Put it down, let go, and try again.")
}

// stopGyro: the aim stops, and a learned drift is kept for the next start.
func (s *session) stopGyro() {
	if s.gyro == nil {
		return
	}
	s.gyro.SetHold(gyro.HoldStart)
	if st := s.gyro.Status(); st.Calibrated && s.wantGyro() {
		if err := gyro.SaveBias(s.biasPath(), st.Bias); err != nil {
			log.Printf("Gyro: %v", err)
			return
		}
		log.Printf("Gyro: drift %.2f %.2f %.2f deg/s, saved", st.Bias[0], st.Bias[1], st.Bias[2])
	}
}
