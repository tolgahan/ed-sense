package diag

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/gyro"
	"github.com/tolgahan/ed-sense/internal/platform"
)

// Gyro is -gyrotest. With the controller at rest it measures the reports
// and the drift, then lets DSX's gyro aim and EDSense's aim in turn while
// the player turns the controller, watching the mouse through Raw Input,
// and prints how the two compare and the settings that match DSX.
func Gyro(b *backend.Backend, cfg config.Config, dataDir string, done <-chan struct{}) {
	if b.Motion == nil || !b.Caps.Gyro {
		fmt.Println("This controller connection passes no gyro.")
		return
	}
	fmt.Println("EDSense gyro test, about 75 seconds. Keep your hand off the mouse: the cursor will move.")
	var forElite, inUse string
	backendAims := "DSX's gyro aims."
	if b.Kind == backend.KindDS4Windows {
		backendAims = "DS4Windows' gyro is off in its profile, so the mouse should stay still."
		fmt.Println("It measures the motion EDSense's gyro aim reads: DS4Windows' UDP server's when it sends, else its virtual DualSense's, " +
			"which drops turns under 2 degrees per second. It needs the DS4Windows profile to leave the gyro alone, as EDSense's gyro aim does.")
	} else {
		forElite, inUse = dsx.Profiles()
	}
	switch {
	case b.Kind == backend.KindDS4Windows:
	case inUse == "":
		fmt.Println("It measures the DSX profile in use (select \"Elite Dangerous\" in DSX if Elite is not running).")
	case forElite != "" && inUse != forElite:
		fmt.Printf("DSX seems to use its profile %q now, and uses %q for Elite, so DSX's numbers will be for %q.\n", inUse, forElite, inUse)
		fmt.Printf("To compare with the Elite one, select %q in DSX (or start Elite), then run the test again.\n", forElite)
	default:
		fmt.Printf("It measures DSX's profile %q.\n", inUse)
	}
	b.Output.RequestStatus()
	b.Pad.Maintain()
	for i := 0; i < 60 && !b.Pad.Available(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !b.Pad.Available() {
		fmt.Println(b.Words.PadTestMissing)
		return
	}
	// DS4Windows' own gyro cannot be switched off from here, so with a
	// profile that uses the gyro both would move the mouse in step 3
	if b.Kind == backend.KindDS4Windows {
		if use := profileGyro(b, dataDir); use != backend.GyroUnused {
			fmt.Println(gyroInUse(use))
			return
		}
	}

	var mu sync.Mutex
	var samples []gyro.Sample
	var moves []platform.MouseMove
	var aim *gyro.Aim // set while EDSense aims
	b.Motion.OnSample(func(s gyro.Sample) {
		mu.Lock()
		samples = append(samples, s)
		a := aim
		mu.Unlock()
		if a != nil {
			a.Feed(s)
		}
	})
	defer b.Motion.OnSample(nil)
	stopWatch := make(chan struct{})
	if err := platform.WatchMouse(stopWatch, func(m platform.MouseMove) {
		mu.Lock()
		moves = append(moves, m)
		mu.Unlock()
	}); err != nil {
		fmt.Printf("Cannot watch the mouse: %v\n", err)
		return
	}
	defer close(stopWatch)
	controllers := b.Output.Controllers()
	setMotion := func(m backend.MotionMode) { b.Output.SetMotion(controllers, m) }
	defer func() {
		b.Output.SetMotion(b.Output.Controllers(), backend.MotionProfile)
		time.Sleep(150 * time.Millisecond) // let the packet go
	}()

	// phase runs one step of the test for d, telling DSX mode now and
	// every 3 s (repeat false: only now), and returns its span. An empty
	// title prints nothing.
	phase := func(title string, d time.Duration, m backend.MotionMode, repeat bool) (GyroSpan, bool) {
		if title != "" {
			fmt.Println("\n" + title)
		}
		setMotion(m)
		from := time.Now()
		for t := time.Duration(0); t < d; t += time.Second {
			if repeat && t > 0 && t%(3*time.Second) == 0 {
				setMotion(m)
			}
			if left := (d - t) / time.Second; left%5 == 0 {
				fmt.Printf("  %d s\n", left)
			}
			if !wait(done, time.Second) {
				return GyroSpan{}, false
			}
		}
		return GyroSpan{from, time.Now()}, true
	}
	snapshot := func(span GyroSpan) []gyro.Sample {
		mu.Lock()
		defer mu.Unlock()
		var out []gyro.Sample
		for _, s := range samples {
			if span.has(s.At) {
				out = append(out, s)
			}
		}
		return out
	}

	// time to put the controller down before the rest starts
	fmt.Println("\n1/4: put the controller down and let go.")
	fmt.Println("  starting in 3 s")
	if !wait(done, 3*time.Second) {
		return
	}
	span, ok := phase("", 5*time.Second, backend.MotionProfile, false)
	if !ok {
		return
	}
	span.From = span.From.Add(restSettle) // the hand may still be leaving it
	rest := restStats(snapshot(span))
	if rest.Reports == 0 {
		fmt.Println("\nNo reports came from the virtual DualSense.")
		return
	}
	if b.MotionState != nil {
		if b.MotionState().Source == backend.SourceUDP {
			fmt.Println("  Motion from DS4Windows' UDP server (no dead band).")
		} else {
			fmt.Println("  Motion from DS4Windows' virtual DualSense: turns under 2 degrees per second are lost. " +
				"Turn on Settings > UDP Server > Enable Server in DS4Windows.")
		}
	}
	biasPath := filepath.Join(dataDir, restBiasFile(b))
	if rest.Still = restStill(rest); rest.Still {
		if err := gyro.SaveBias(biasPath, rest.Bias); err != nil {
			fmt.Printf("  Could not save the calibration: %v\n", err)
		} else {
			rest.Saved = true
		}
	}
	// One drift for EDSense's aim and the analysis: the rest's, else the
	// saved one, which the tray's gyro would use, else none.
	bias, driftSaved := rest.Bias, false
	if !rest.Still {
		bias, driftSaved = gyro.LoadBias(biasPath)
	}

	const moveHint = "Turn left and right slowly, then fast; tilt up and down slowly, then fast; roll left and right."
	dsxSpan, ok := phase("2/4: "+backendAims+" "+moveHint, 25*time.Second, backend.MotionToMouse, true)
	if !ok {
		return
	}

	settings := gyro.Settings{
		CountsPerDeg:  [2]float64{gyro.DSXCountsPerDegree * cfg.GyroSensitivityX, gyro.DSXCountsPerDegree * cfg.GyroSensitivityY},
		RollMix:       cfg.GyroRollMix,
		Exact:         cfg.GyroLowSpeed == config.GyroLowExact,
		AutoCalibrate: false,
	}
	a := gyro.New(gyro.MouseFunc(platform.MoveMouse))
	a.SetSettings(settings)
	a.SetBias(bias)
	a.SetHold(0)
	mu.Lock()
	aim = a
	mu.Unlock()
	ownSpan, ok := phase("3/4: EDSense's gyro aims. The same moves again.", 25*time.Second, backend.MotionNone, true)
	mu.Lock()
	aim = nil
	mu.Unlock()
	a.SetHold(gyro.HoldStart)
	if !ok {
		return
	}

	touchSpan, ok := phase("4/4: rest a finger on the touchpad and move the controller; lift it and move again; twice.", 12*time.Second, backend.MotionNone, false)
	if !ok {
		return
	}

	mu.Lock()
	rec := GyroRecording{Motion: motionOf(samples, bias), DSX: dsxSpan, Own: ownSpan, Touch: touchSpan}
	for _, m := range moves {
		rec.Mouse = append(rec.Mouse, GyroMouse(m))
	}
	mu.Unlock()
	r := AnalyseGyro(rec)
	r.Rest, r.Want, r.DriftSaved = rest, settings.CountsPerDeg, driftSaved
	r.DSXProfile, r.EliteProfile = inUse, forElite
	if r.EliteRuns = platform.ProcessRunning(elite.GameExe); r.EliteRuns {
		r.EliteBlocked = platform.InputBlocked(elite.GameExe)
	}
	fmt.Println()
	r.Print(os.Stdout)
}

// profileGyro is what the backend's profile does with the gyro, once its
// first check is done.
func profileGyro(b *backend.Backend, dataDir string) backend.GyroUse {
	if b.NewSetup == nil {
		return backend.GyroUnknown
	}
	s := b.NewSetup(dataDir, func(msg string) { fmt.Println(msg) })
	if c, ok := s.(interface{ Close() }); ok {
		defer c.Close()
	}
	if c, ok := s.(backend.Checker); ok {
		select {
		case <-c.Checked():
		case <-time.After(20 * time.Second):
		}
	}
	return s.Gyro()
}

// gyroInUse tells why -gyrotest stops under DS4Windows.
func gyroInUse(use backend.GyroUse) string {
	why := "EDSense cannot tell what the DS4Windows profile does with the gyro (the log's DS4Windows: line says why), so EDSense's gyro stays off."
	switch use {
	case backend.GyroMouse:
		why = "The DS4Windows profile moves the mouse with the gyro, so EDSense's gyro stays off, and in this test both would move the mouse."
	case backend.GyroElsewhere:
		why = "The DS4Windows profile uses the gyro for a stick or swipes, so EDSense's gyro stays off."
	}
	return why + "\nSet the profile's Gyro -> Output Mode to Passthru in DS4Windows, save it, and run this test again."
}

// restBiasFile is the calibration file of the motion the rest measured:
// under DS4Windows, its UDP server's or its virtual DualSense's.
func restBiasFile(b *backend.Backend) string {
	src := backend.SourcePad
	if b.MotionState != nil && b.MotionState().Source == backend.SourceUDP {
		src = backend.SourceUDP
	}
	return b.BiasFileFor(src)
}

// restSettle: the first second of the rest is left out.
const restSettle = time.Second

// restStill: enough reports carried motion, and the controller lay still
// enough for their mean to be the drift. Empty reports have no drift to
// measure.
func restStill(r GyroRest) bool {
	for i := range 3 {
		if r.Noise[i] > 5 || math.Abs(r.Bias[i]) > 3 {
			return false
		}
	}
	return r.Reports-r.Empty > 100
}

// restStats measures phase 1: the report rate and clock, and the drift.
func restStats(ss []gyro.Sample) GyroRest {
	var r GyroRest
	r.Reports = len(ss)
	if len(ss) < 2 {
		return r
	}
	var gaps []time.Duration
	var ticks float64
	var min, max [3]float64
	var sum [3]float64
	var gravity float64
	n := 0
	for i, s := range ss {
		if s.Gyro == ([3]float64{}) && s.Accel == ([3]float64{}) {
			r.Empty++
		} else {
			for k := range 3 {
				if n == 0 || s.Gyro[k] < min[k] {
					min[k] = s.Gyro[k]
				}
				if n == 0 || s.Gyro[k] > max[k] {
					max[k] = s.Gyro[k]
				}
				sum[k] += s.Gyro[k]
			}
			gravity += math.Sqrt(s.Accel[0]*s.Accel[0] + s.Accel[1]*s.Accel[1] + s.Accel[2]*s.Accel[2])
			n++
		}
		if i == 0 {
			continue
		}
		gap := s.At.Sub(ss[i-1].At)
		gaps = append(gaps, gap)
		if gap > 0 && (r.Step == 0 || gap < r.Step) {
			r.Step = gap
		}
		d := s.Stamp - ss[i-1].Stamp
		if d == 0 {
			r.Repeats++
		}
		ticks += float64(d)
	}
	wall := ss[len(ss)-1].At.Sub(ss[0].At).Seconds()
	if wall > 0 {
		r.PerSecond = float64(len(ss)-1) / wall
		if ss[0].StampHz > 0 {
			r.ClockHz = ticks / wall
		}
	}
	slices.Sort(gaps)
	r.Median, r.P95, r.Max = gaps[len(gaps)/2], gaps[len(gaps)*95/100], gaps[len(gaps)-1]
	if n > 0 {
		for k := range 3 {
			r.Bias[k] = sum[k] / float64(n)
			r.Noise[k] = max[k] - min[k]
		}
		r.Gravity = gravity / float64(n)
	}
	return r
}

// motionOf turns the samples into what the analysis reads: the rates
// without the drift, and dt by the sensor clock, 0 for a repeat or a gap.
func motionOf(ss []gyro.Sample, bias [3]float64) []GyroMotion {
	out := make([]GyroMotion, len(ss))
	for i, s := range ss {
		m := GyroMotion{At: s.At, Touch: s.Touch, Empty: s.Gyro == ([3]float64{}) && s.Accel == ([3]float64{})}
		for k := range 3 {
			m.Rate[k] = s.Gyro[k] - bias[k]
		}
		if i > 0 && s.StampHz > 0 {
			if d := s.Stamp - ss[i-1].Stamp; d > 0 {
				if dt := float64(d) / s.StampHz; dt <= 0.05 {
					m.Dt = dt
				}
			}
		}
		out[i] = m
	}
	return out
}
