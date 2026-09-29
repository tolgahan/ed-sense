package diag

import (
	"bytes"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/gyro"
)

var gyroStart = time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)

const reportDt = 0.004 // 250 Hz

// dsxRef is DSX 3.2.0's motion to mouse with its bundled profile, as read
// from DSX.dll (platform.md 4.3), written here apart from EDSense's gyro so
// the fit has something independent to find again.
type dsxRef struct {
	perDeg float64 // counts per degree
	exact  bool    // no floors and no decay: a stand-in for EDSense's aim
	invert [3]bool // pitch, yaw, roll turned around, as a profile may have them
	rem    [2]float64
}

func newDSXRef() *dsxRef { return &dsxRef{perDeg: 16201.0 / 360 * 0.5} } // 22.5014

func (d *dsxRef) step(w [3]float64, dt float64) (dx, dy int32) {
	for i, flip := range d.invert {
		if flip {
			w[i] = -w[i]
		}
	}
	if !d.exact {
		for i := range w {
			if math.Abs(w[i]) < 0.375 { // per axis, deg/s
				w[i] = 0
			}
		}
	}
	move := [2]float64{-(w[1] + 0.6*w[2]) * dt * d.perDeg, -w[0] * dt * d.perDeg}
	var out [2]int32
	for i, v := range move {
		if !d.exact && math.Abs(v) < 0.1 { // per report, counts
			v = 0
		}
		d.rem[i] += v
		n := math.Trunc(d.rem[i])
		d.rem[i] -= n
		if !d.exact {
			d.rem[i] *= 0.94
		}
		out[i] = int32(n)
	}
	return out[0], out[1]
}

// gyroScript: the moves -gyrotest asks for, t s in, as deg/s of pitch, yaw
// and roll: rest, slow turns, faster ones, tilting, rolling, then all at once.
func gyroScript(t float64) [3]float64 {
	wave := func(amp, period, from float64) float64 { return amp * math.Sin(2*math.Pi*(t-from)/period) }
	switch {
	case t < 1:
		return [3]float64{}
	case t < 5:
		return [3]float64{0, 1, 0} // under DSX's per-report floor
	case t < 7:
		return [3]float64{0, -1.6, 0} // about 80% gets through
	case t < 9:
		return [3]float64{0, 3.5, 0}
	case t < 11:
		return [3]float64{0, -7, 0}
	case t < 13:
		return [3]float64{0, 20, 0}
	case t < 16:
		return [3]float64{0, wave(90, 1.5, 13), 0}
	case t < 19:
		return [3]float64{wave(60, 1.5, 16), 0, 0}
	case t < 22:
		return [3]float64{0, 0, wave(80, 1.5, 19)}
	case t < 26:
		return [3]float64{wave(40, 1.1, 22), wave(70, 1.7, 22), wave(50, 1.3, 22)}
	}
	return [3]float64{}
}

// record plays script from start for d through the reference, which moves
// the mouse delay after each report, with a little seeded noise on the gyro.
// Jitter delays each report's and each mouse move's arrival by up to that
// much, as a busy Windows might.
func record(rec *GyroRecording, start time.Time, d time.Duration, script func(float64) [3]float64,
	ref *dsxRef, delay, jitter time.Duration, ours bool) {
	rng := rand.New(rand.NewPCG(1, 2))
	late := func() time.Duration {
		if jitter == 0 {
			return 0
		}
		return time.Duration(rng.Float64() * float64(jitter))
	}
	for i := range int(d.Seconds() / reportDt) {
		at := start.Add(time.Duration(i) * 4 * time.Millisecond)
		w := script(float64(i) * reportDt)
		for k := range w {
			w[k] += 0.2 * (rng.Float64() - 0.5)
		}
		rec.Motion = append(rec.Motion, GyroMotion{At: at.Add(late()), Rate: w, Dt: reportDt})
		if dx, dy := ref.step(w, reportDt); dx != 0 || dy != 0 {
			rec.Mouse = append(rec.Mouse, GyroMouse{DX: dx, DY: dy, Injected: true, Ours: ours, At: at.Add(delay + late())})
		}
	}
}

func TestFitRecoversDSX(t *testing.T) {
	var rec GyroRecording
	record(&rec, gyroStart, 27*time.Second, gyroScript, newDSXRef(), 8*time.Millisecond, 0, false)
	rec.DSX = GyroSpan{gyroStart, gyroStart.Add(27 * time.Second)}
	r := AnalyseGyro(rec)
	f := r.DSX
	t.Logf("K %.3f roll %.3f V %.3f lag %v leak %.3f windows %d low %.2f", f.Side, f.Roll, f.Up, f.Lag, f.Leak, f.Windows, f.Low)
	// DSX's decaying remainder costs it about 7.5 counts a second at any
	// speed, so at these speeds the gains read about 1% under 22.5
	if math.Abs(f.Side-22.5) > 0.3 {
		t.Errorf("sideways %.2f counts/deg, want 22.5", f.Side)
	}
	if math.Abs(f.Roll-0.6) > 0.03 {
		t.Errorf("roll mix %.3f, want 0.6", f.Roll)
	}
	if math.Abs(f.Up-22.5) > 0.3 {
		t.Errorf("up and down %.2f counts/deg, want 22.5", f.Up)
	}
	if f.Lag < 0 || f.Lag > 10*time.Millisecond {
		t.Errorf("lag %v, want 0 to 10 ms", f.Lag)
	}
	if math.Abs(f.Leak) > 0.5 {
		t.Errorf("pitch leaks %.2f counts/deg into sideways", f.Leak)
	}
	if !(f.Low[0] < 0.5) {
		t.Errorf("under 2 deg/s DSX delivers %.2f, want under half", f.Low[0])
	}
	for i := 1; i < len(f.Low); i++ {
		if !(f.Low[i] > f.Low[0] && f.Low[i] < 1.02) {
			t.Errorf("band %d delivers %.2f", i, f.Low[i])
		}
	}
	if r.OwnDSX != 0 || r.Lifts != 0 || r.Device != 0 {
		t.Errorf("no other phases recorded: %+v", r)
	}
	var out bytes.Buffer
	r.Print(&out)
	t.Log("\n" + out.String())
	if !strings.Contains(out.String(), `"gyro_roll_mix": 0.60`) {
		t.Error("the suggested settings match DSX's profile")
	}
}

// DSX may move the mouse before EDSense reads the report or well after, and
// Windows delivers both a little late at random.
func TestGyroLag(t *testing.T) {
	for _, delay := range []time.Duration{-20 * time.Millisecond, 8 * time.Millisecond, 50 * time.Millisecond} {
		var rec GyroRecording
		record(&rec, gyroStart, 27*time.Second, gyroScript, newDSXRef(), delay, 3*time.Millisecond, false)
		rec.DSX = GyroSpan{gyroStart, gyroStart.Add(27 * time.Second)}
		f := AnalyseGyro(rec).DSX
		if d := f.Lag - delay; d < -3*time.Millisecond || d > 3*time.Millisecond {
			t.Errorf("DSX's mouse %v after the report, measured %v", delay, f.Lag)
		}
		if math.Abs(f.Side-22.5) > 0.3 || math.Abs(f.Up-22.5) > 0.3 {
			t.Errorf("with the mouse %v late: %.2f sideways, %.2f up and down", delay, f.Side, f.Up)
		}
	}
}

// A DSX profile with an axis turned around: the printout says which way
// EDSense's gyro would differ, and suggests nothing it cannot match.
func TestGyroSigns(t *testing.T) {
	fast := func(t float64) [3]float64 { return gyroScript(t + 13) }
	for _, c := range []struct {
		invert    [3]bool
		want, not []string
	}{
		{[3]bool{false, true, false},
			[]string{"sideways the other way"},
			[]string{"Rolling", "up and down the other way", "Suggested"}},
		{[3]bool{false, true, true}, // DSX's horizontal inverted: yaw and roll
			[]string{"sideways the other way", "Rolling moves DSX's mouse the other way"},
			[]string{"Suggested"}},
		{[3]bool{false, false, true},
			[]string{"Rolling moves DSX's mouse the other way", `"gyro_sensitivity_x": 0.99, "gyro_sensitivity_y": 0.99`},
			[]string{"sideways the other way", "gyro_roll_mix"}},
		{[3]bool{true, false, false},
			[]string{"up and down the other way", `"gyro_sensitivity_x": 0.99, "gyro_roll_mix": 0.60`},
			[]string{"sideways the other way", "Rolling", "gyro_sensitivity_y"}},
	} {
		var rec GyroRecording
		ref := newDSXRef()
		ref.invert = c.invert
		record(&rec, gyroStart, 13*time.Second, fast, ref, 8*time.Millisecond, 0, false)
		rec.DSX = GyroSpan{gyroStart, gyroStart.Add(13 * time.Second)}
		var out bytes.Buffer
		AnalyseGyro(rec).Print(&out)
		text := out.String()
		for _, s := range c.want {
			if !strings.Contains(text, s) {
				t.Errorf("inverted %v: the printout lacks %q:\n%s", c.invert, s, text)
			}
		}
		for _, s := range c.not {
			if strings.Contains(text, s) {
				t.Errorf("inverted %v: the printout has %q:\n%s", c.invert, s, text)
			}
		}
	}
}

func TestGyroPhases(t *testing.T) {
	var rec GyroRecording
	own := gyroStart
	rec.Own = GyroSpan{own, own.Add(5 * time.Second)}
	turn := func(t float64) [3]float64 {
		return [3]float64{30 * math.Sin(2*math.Pi*t/1.3), 60 * math.Sin(2*math.Pi*t/1.1), 40 * math.Sin(2*math.Pi*t/1.7)}
	}
	ref := &dsxRef{perDeg: 27, exact: true}
	record(&rec, own, 5*time.Second, turn, ref, time.Millisecond, 0, true)
	rec.Motion[100].Empty = true // under NONE the motion should keep coming
	rec.Motion[200].Empty = true
	rec.Motion[300].Touch, rec.Motion[300].Empty = true, true // the touch pause is not NONE's fault
	rec.Mouse = append(rec.Mouse,
		GyroMouse{DX: 9, Injected: true, At: own.Add(100 * time.Millisecond)}, // DSX still taking NONE
		GyroMouse{DX: 2, DY: -1, Injected: true, At: own.Add(2 * time.Second)},
		GyroMouse{DX: 60, At: own.Add(3 * time.Second)}, // a hand on the mouse
	)

	// phase 4: a touch that DSX zeroes, a lift, a touch with live motion,
	// a lift; DSX's mouse moves after the first lift and during the second
	// touch
	touch := own.Add(6 * time.Second)
	rec.Touch = GyroSpan{touch, touch.Add(4 * time.Second)}
	for i := range 1000 { // 4 ms each
		m := GyroMotion{At: touch.Add(time.Duration(i) * 4 * time.Millisecond), Rate: [3]float64{0, 5, 0}, Dt: reportDt}
		switch {
		case i >= 125 && i < 375:
			m.Touch, m.Empty, m.Rate = true, true, [3]float64{}
		case i >= 625 && i < 750:
			m.Touch = true
		}
		rec.Motion = append(rec.Motion, m)
	}
	rec.Mouse = append(rec.Mouse,
		GyroMouse{DX: 5, Injected: true, At: touch.Add(1600 * time.Millisecond)},
		GyroMouse{DX: 7, Injected: true, At: touch.Add(2700 * time.Millisecond)},
		GyroMouse{DX: -4, Injected: true, At: touch.Add(3500 * time.Millisecond)},
		GyroMouse{DX: 3, Injected: true, Ours: true, At: touch.Add(3600 * time.Millisecond)},
	)

	r := AnalyseGyro(rec)
	if math.Abs(r.Own.Side-27) > 0.3 || math.Abs(r.Own.Up-27) > 0.3 || math.Abs(r.Own.Roll-0.6) > 0.03 {
		t.Errorf("EDSense's gains: %.2f sideways, roll %.3f, %.2f up and down; want 27, 0.6, 27", r.Own.Side, r.Own.Roll, r.Own.Up)
	}
	if r.Own.Lag < 0 || r.Own.Lag > 5*time.Millisecond {
		t.Errorf("EDSense's lag %v", r.Own.Lag)
	}
	if r.OwnDSX != 3 || r.OwnEmpty != 2 || r.OwnReports != 1186 {
		t.Errorf("while EDSense aimed: DSX %d counts, %d of %d reports empty; want 3, 2 of 1186", r.OwnDSX, r.OwnEmpty, r.OwnReports)
	}
	if r.Touched != 375 || r.TouchEmpty != 250 || r.Lifts != 2 || r.LiftDSX != 9 {
		t.Errorf("touch: %d reports, %d empty, %d lifts, DSX %d counts after; want 375, 250, 2, 9", r.Touched, r.TouchEmpty, r.Lifts, r.LiftDSX)
	}
	if r.Device != 60 {
		t.Errorf("real mouse %d counts, want 60", r.Device)
	}
	if !math.IsNaN(r.DSX.Side) || r.DSX.Counts != 0 {
		t.Errorf("no DSX phase: %+v", r.DSX)
	}

	r.Rest = GyroRest{Reports: 1250, PerSecond: 250, Median: 4 * time.Millisecond, P95: 4400 * time.Microsecond,
		Max: 12 * time.Millisecond, Step: 500 * time.Microsecond, ClockHz: 3e6, Gravity: 1, Still: true, Saved: true}
	r.Want = [2]float64{22.5, 22.5}
	r.EliteRuns = true
	var out bytes.Buffer
	r.Print(&out)
	text := out.String()
	t.Log("\n" + text)
	for _, want := range []string{
		"sensor clock 3.00 MHz",
		"Calibration saved.",
		"DSX: the controller hardly moved",
		"(the settings ask for 22.5 and 22.5)",
		"DSX meanwhile: 3 counts (its mouse is still on",
		"2 empty reports of 1186",
		"Touch: 250 of 375 reports empty while touching; after lifting DSX moved the mouse 9 counts",
		"Elite: runs as your user",
		"The mouse was moved during the test (60 counts)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the printout lacks %q", want)
		}
	}
	if strings.Contains(text, "NaN") || strings.Contains(text, "Suggested") {
		t.Error("nothing unmeasured is printed or suggested")
	}
	for _, c := range text {
		if c > 127 {
			t.Fatalf("non-ASCII %q in the printout", c)
		}
	}
}

// A DSX profile that passes no motion on (Passthrough off) empties every
// report: nothing is saved over a good calibration, and the printout says
// why instead of asking for faster turns.
func TestGyroNoMotion(t *testing.T) {
	var rest []gyro.Sample
	for i := range 1000 {
		rest = append(rest, gyro.Sample{Stamp: uint32(i * 12000), StampHz: 3e6, At: gyroStart.Add(time.Duration(i) * 4 * time.Millisecond)})
	}
	r := restStats(rest)
	if r.Reports != 1000 || r.Empty != 1000 || restStill(r) {
		t.Fatalf("all empty: %d reports, %d empty, still %v", r.Reports, r.Empty, restStill(r))
	}

	var rec GyroRecording
	rec.DSX = GyroSpan{gyroStart, gyroStart.Add(5 * time.Second)}
	for i := range 1250 {
		at := gyroStart.Add(time.Duration(i) * 4 * time.Millisecond)
		rec.Motion = append(rec.Motion, GyroMotion{At: at, Empty: true, Dt: reportDt})
		if i%10 == 0 { // DSX's mouse moves from the real controller
			rec.Mouse = append(rec.Mouse, GyroMouse{DX: 20, Injected: true, At: at})
		}
	}
	res := AnalyseGyro(rec)
	res.Rest = r
	if res.DSXEmpty != 1250 || res.DSXReports != 1250 {
		t.Errorf("DSX phase: %d of %d empty", res.DSXEmpty, res.DSXReports)
	}
	var out bytes.Buffer
	res.Print(&out)
	text := out.String()
	t.Log("\n" + text)
	for _, want := range []string{
		"At rest: the reports carry no motion",
		"No calibration was saved. No drift is taken off below",
		"(1250 of 1250 reports empty)",
		"Turn on Passthrough",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the printout lacks %q", want)
		}
	}
	for _, not := range []string{"faster", "Suggested", "drift 0.00", "NaN"} {
		if strings.Contains(text, not) {
			t.Errorf("the printout has %q", not)
		}
	}
}

// A rest that moved, with a saved calibration: the printout says which
// drift the numbers use.
func TestGyroRestMoved(t *testing.T) {
	res := GyroResult{Rest: GyroRest{Reports: 1000, PerSecond: 250, Bias: [3]float64{0, 12, 0}, Noise: [3]float64{2, 40, 3}, Gravity: 1}, DriftSaved: true}
	if restStill(res.Rest) {
		t.Fatal("a rest that turned 12 deg/s counted as still")
	}
	var out bytes.Buffer
	res.Print(&out)
	if text := out.String(); !strings.Contains(text, "The controller moved, so no calibration was saved. The saved calibration is used below.") {
		t.Errorf("printout:\n%s", text)
	}
}

// DSX measured with another profile than the one it uses for Elite: its
// numbers are shown, but nothing is suggested from them.
func TestGyroOtherProfile(t *testing.T) {
	res := GyroResult{
		DSX:        GyroFit{Counts: 5000, Turned: 300, Side: 34.5, Roll: 0.8, Up: 39.5, Windows: 40},
		Own:        GyroFit{Side: math.NaN(), Up: math.NaN(), Roll: math.NaN()},
		DSXProfile: "Default Profile", EliteProfile: "Elite Dangerous",
	}
	var out bytes.Buffer
	res.Print(&out)
	text := out.String()
	if strings.Contains(text, "Suggested") || !strings.Contains(text, `DSX's numbers are for its profile "Default Profile"; in Elite it uses "Elite Dangerous"`) {
		t.Errorf("printout:\n%s", text)
	}
	res.DSXProfile = "Elite Dangerous"
	out.Reset()
	res.Print(&out)
	if text := out.String(); !strings.Contains(text, `"gyro_sensitivity_x": 1.53`) {
		t.Errorf("same profile, printout:\n%s", text)
	}
}
