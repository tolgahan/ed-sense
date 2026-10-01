package app

import (
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// EDSense's own gyro aim. The gyro package turns every motion report into
// mouse movement on the backend's reading goroutine. The loop decides when
// it may aim, keeps DSX's motion to mouse off meanwhile, and leaves the aim
// to DSX's gyro when EDSense's cannot work: no motion data, or Elite running
// as administrator. DS4Windows' gyro cannot be switched off from here, so
// there EDSense aims only while the DS4Windows profile leaves the gyro
// alone.

// gyroSettings: the settings file's gyro keys.
func gyroSettings(c *config.Config) gyro.Settings {
	return gyro.Settings{
		CountsPerDeg:  [2]float64{gyro.DSXCountsPerDegree * c.GyroSensitivityX, gyro.DSXCountsPerDegree * c.GyroSensitivityY},
		RollMix:       c.GyroRollMix,
		Exact:         c.GyroLowSpeed == config.GyroLowExact,
		AutoCalibrate: c.GyroAutoCalibrate,
	}
}

// biasPathFor is the file of the drift of src's motion.
func (a *App) biasPathFor(src backend.MotionSource) string {
	name := a.bias
	if f := a.b.BiasFileFor(src); src == backend.SourceUDP && f != "" {
		name = f
	}
	return filepath.Join(a.dataDir(), name)
}

// saveBias keeps the drift for the motion source the gyro holds it for.
func (a *App) saveBias(b [3]float64) error {
	if err := gyro.SaveBias(a.biasPathFor(a.biasSrc), b); err != nil {
		return err
	}
	a.biasSaved, a.biasGuess = b, false
	return nil
}

// sourceName is a motion source, for the log.
func sourceName(src backend.MotionSource) string {
	if src == backend.SourceUDP {
		return "DS4Windows' UDP server"
	}
	return "DS4Windows' virtual DualSense"
}

// followMotion: where the backend's motion comes from this tick. When it
// moves between the virtual DualSense and DS4Windows' UDP server, the
// drift learned for the one is saved, and the other's loaded, or learned
// again: the virtual DualSense's dead band hides the drift the UDP server
// shows. A source with no drift saved starts at 0, held as known: the
// switch comes mostly in flight, where a slow pan must not be taken for
// drift, so the guard of a known drift stays on while the gyro aims.
func (s *session) followMotion() backend.MotionState {
	s.ms = backend.MotionState{}
	if s.b.MotionState == nil || s.gyro == nil {
		return s.ms
	}
	s.ms = s.b.MotionState()
	src := s.ms.Source
	if src != backend.SourcePad && src != backend.SourceUDP || src == s.biasSrc {
		return s.ms
	}
	if st := s.gyro.Status(); st.Calibrated && s.wantGyro() && st.Bias != s.biasSaved {
		if err := s.saveBias(st.Bias); err != nil {
			log.Printf("Gyro: %v", err)
		} else {
			log.Printf("Gyro: drift %.2f %.2f %.2f deg/s, saved", st.Bias[0], st.Bias[1], st.Bias[2])
		}
	}
	s.biasSrc = src
	s.gyro.Forget() // the stillness seen so far was the other source's
	if b, ok := gyro.LoadBias(s.biasPathFor(src)); ok {
		s.gyro.SetBias(b)
		s.biasSaved, s.biasGuess = b, false
		log.Printf("Gyro: drift for %s loaded (%.2f %.2f %.2f deg/s)", sourceName(src), b[0], b[1], b[2])
	} else {
		s.gyro.SetBias([3]float64{})
		s.biasSaved, s.biasGuess = [3]float64{}, true
		log.Printf("Gyro: no drift known yet for %s; it is learned when the controller lies still", sourceName(src))
	}
	return s.ms
}

// checkMotion, under DS4Windows: once the gyro has aimed for 10 s on the
// virtual DualSense's motion while the UDP server is silent, or answers
// for this controller and sends none of its motion, the player is told how
// to get the slow turns back; and the log says once when the UDP server
// answers without this controller.
func (s *session) checkMotion(own bool) {
	if s.b.MotionState == nil {
		return
	}
	if s.ms.UDP != backend.UDPNoData {
		s.noDataFor = 0 // only a lasting one counts: it comes for a moment at each switch
	}
	if !own || s.hold != 0 {
		return
	}
	step := time.Duration(s.cfg.PollMs) * time.Millisecond
	if s.ms.Source != backend.SourceUDP && s.ms.UDP == backend.UDPNoData {
		s.noDataFor += step
		if s.noDataFor >= 10*time.Second && !s.told.udpNoData && s.words.UDPNoDataLog != "" {
			s.told.udpNoData = true
			log.Print(s.words.UDPNoDataLog)
			s.tell(s.words.UDPNoDataTell)
		}
	}
	if s.ms.Source == backend.SourcePad && s.ms.UDP == backend.UDPSilent {
		s.padAim += step
		if s.padAim >= 10*time.Second && !s.told.slowGyro && s.words.SlowGyroLog != "" {
			s.told.slowGyro = true
			log.Print(s.words.SlowGyroLog)
			s.tell(s.words.SlowGyroTell)
		}
	}
	if s.ms.UDP == backend.UDPOther {
		s.otherFor += step
		if s.otherFor >= 10*time.Second && !s.told.udpOther && s.words.UDPOtherLog != "" {
			s.told.udpOther = true
			log.Print(s.words.UDPOtherLog)
		}
	}
}

// CalibrateGyro learns the gyro's drift from the next 2 s, with the
// controller lying still. It runs on the tray's goroutine, so it only asks
// the loop, which starts it and tells the result.
func (a *App) CalibrateGyro() {
	if !a.ident.Load().hasGyro {
		a.tell(noGyro)
		return
	}
	request(a.calibrateRequests)
}

const noGyro = "This controller connection passes no gyro."

// startCalibration: the loop takes a calibration asked for from the tray.
// Meanwhile DSX passes the motion on without moving the mouse, in menus
// and paused too (motionPolicy).
func (s *session) startCalibration() {
	if s.gyro == nil { // asked for before a restart onto a backend without one
		s.tell(noGyro)
		return
	}
	if !s.running {
		s.tell("EDSense reads the controller while Elite runs. Start Elite, then try again.")
		return
	}
	s.pad.Maintain()
	if !s.pad.Available() {
		s.tell(s.words.NoPadToCalibrate)
		return
	}
	log.Print("Gyro: calibrating")
	s.gyro.Calibrate(2 * time.Second)
}

// wantGyro: the settings give the aim to EDSense's gyro, and the DSX
// profile's gyro is motion to mouse, or can't be read. EDSense's gyro
// replaces only that, never a gyro the player has on a stick or keys.
// DS4Windows' gyro cannot be switched off, so there the profile must leave
// the gyro alone, and a profile that can't be read keeps EDSense's off.
func (s *session) wantGyro() bool {
	if s.gyro == nil || s.cfg.GyroBy != config.GyroByEDSense {
		return false
	}
	use := s.profile.Gyro()
	if !s.caps.MotionOff {
		return use == backend.GyroUnused
	}
	return use == backend.GyroMouse || use == backend.GyroUnknown
}

// noteProfile logs once when the profile keeps the aim from EDSense's
// gyro.
func (s *session) noteProfile(now time.Time) {
	use := s.profile.Gyro()
	if !s.caps.MotionOff {
		s.noteDS4WindowsProfile(now, use)
		return
	}
	keep := s.gyro != nil && s.cfg.GyroBy == config.GyroByEDSense && use == backend.GyroElsewhere
	if keep && !s.saidProfile {
		log.Print(s.words.ProfileKeeps)
	}
	s.saidProfile = keep
}

// profileSettles: Elite in front this long, DS4Windows' Auto Profiles
// have given it its profile (they look every second), and a check made
// since (every 3 s, and when Elite comes to the front) has read it.
const profileSettles = 6 * time.Second

// noteDS4WindowsProfile logs what the DS4Windows profile's gyro means for
// EDSense's whenever that changes, and tries the motion data again. The
// profile is read in the first seconds, so it is not called unknown
// before.
func (s *session) noteDS4WindowsProfile(now time.Time, use backend.GyroUse) {
	if !s.running || !s.front() {
		s.frontSince = time.Time{}
	} else if s.frontSince.IsZero() {
		s.frontSince = now
	}
	// with Elite behind, the profile read is the one DS4Windows gives the
	// window in front
	settled := !s.frontSince.IsZero() && now.Sub(s.frontSince) >= profileSettles
	if s.gyro != nil && settled && s.cfg.GyroAim && s.cfg.GyroBy != config.GyroByEDSense && use == backend.GyroUnused &&
		s.words.OwnGyroOffTell != "" && !s.told.ownGyroOff {
		// DS4Windows' own gyro cannot be switched on from here
		s.told.ownGyroOff = true
		log.Print(s.words.OwnGyroOffLog)
		s.tell(s.words.OwnGyroOffTell)
	}
	if s.gyro == nil || s.cfg.GyroBy != config.GyroByEDSense {
		s.saidUse = -1
		return
	}
	if use == s.saidUse || use == backend.GyroUnknown && now.Sub(s.startedAt) < 5*time.Second {
		return
	}
	msg := s.words.ProfileKeeps
	switch use {
	case backend.GyroUnused:
		msg = ""
		s.gyroNoData = false // the player may have fixed the profile
	case backend.GyroUnknown:
		msg = s.words.ProfileUnknown
	}
	if msg != "" {
		log.Print(msg)
	}
	s.saidUse = use
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
		log.Print(s.words.ElevatedLog)
		s.tell(s.words.ElevatedTell)
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
	if s.caps.MotionOff {
		s.gyro.SetDSXAims(s.running && s.motion == motionProfile)
	} else {
		s.gyro.SetDSXAims(s.running && s.profile.Gyro() != backend.GyroUnused)
	}
	s.noteProfile(now)
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
	if s.caps.MotionOff && h == 0 && (!online || s.motion != motionNone || len(s.controllers) == 0) {
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
	s.checkOwnGyro(now, own, st)
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
// passes it on (with DS4Windows: to no gyro aim, while the pad is open),
// and logs what the aim learns or cannot do.
func (s *session) checkOwnGyro(now time.Time, own bool, st gyro.Status) {
	noData := s.motion == motionNone && len(s.controllers) > 0 && now.Sub(s.noneSince) > time.Second
	if !s.caps.MotionOff {
		noData = own
	}
	if !s.gyroNoData && noData && st.ZeroFor >= 2*time.Second {
		s.gyroNoData = true
		log.Print(s.words.NoDataLog)
		s.tell(s.words.NoDataTell)
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
		if err := s.saveBias(st.Bias); err != nil {
			log.Printf("Gyro: %v", err)
		}
		s.tell("Gyro calibrated.")
		return
	}
	if st.ManualNoData {
		log.Print("Gyro: calibration failed, no motion data came")
		s.tell(s.words.CalibrateNoData)
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
	// a drift of 0 held for a source with none saved is no drift learned
	if st := s.gyro.Status(); st.Calibrated && s.wantGyro() && !(s.biasGuess && st.Bias == s.biasSaved) {
		if err := s.saveBias(st.Bias); err != nil {
			log.Printf("Gyro: %v", err)
			return
		}
		log.Printf("Gyro: drift %.2f %.2f %.2f deg/s, saved", st.Bias[0], st.Bias[1], st.Bias[2])
	}
}
