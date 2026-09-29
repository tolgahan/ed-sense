package diag

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/gyro"
)

// The -gyrotest analysis: from the motion reports and the mouse moves the
// test recorded, how far DSX's gyro and EDSense's move the mouse per degree,
// how late DSX moves it, how much of a slow turn gets through, and what DSX
// does while EDSense aims. Nothing here touches Windows or the controller.

// GyroMotion is one motion report as -gyrotest recorded it.
type GyroMotion struct {
	At    time.Time  // when EDSense read the report
	Rate  [3]float64 // deg/s with the bias removed: X pitch, Y yaw, Z roll
	Dt    float64    // s, by the gyro's clock rule; 0: a skipped report
	Touch bool       // a finger rests on the touchpad
	Empty bool       // all motion bytes zero (DSX's touch pause, or its motion off)
}

// GyroMouse is one relative mouse report seen through Raw Input. Same
// fields as platform.MouseMove, so GyroMouse(m) converts one.
type GyroMouse struct {
	DX, DY   int32
	Injected bool // sent with SendInput (DSX's or EDSense's), not a real mouse
	Ours     bool // sent by EDSense
	At       time.Time
}

// GyroSpan is one phase of the test, From included, To not.
type GyroSpan struct{ From, To time.Time }

func (s GyroSpan) has(t time.Time) bool { return !t.Before(s.From) && t.Before(s.To) }

// GyroRecording is what -gyrotest recorded, each slice in arrival order.
type GyroRecording struct {
	Motion []GyroMotion
	Mouse  []GyroMouse
	DSX    GyroSpan // phase 2: DSX aims
	Own    GyroSpan // phase 3: EDSense aims, DSX's mouse is off
	Touch  GyroSpan // phase 4: touching and lifting, DSX's mouse still off
}

// GyroRest is phase 1, the controller at rest. The runner measures it,
// because it needs the bias before EDSense aims in phase 3.
type GyroRest struct {
	Reports          int
	PerSecond        float64       // reports per second
	Median, P95, Max time.Duration // time between reports
	Step             time.Duration // the smallest non-zero time between reports: how fine the clock is
	ClockHz          float64       // sensor clock ticks per second of wall time; 0: no sensor clock
	Repeats          int           // reports with the same sensor clock as the one before
	Empty            int           // reports with all motion bytes zero
	Bias             [3]float64    // deg/s
	Noise            [3]float64    // deg/s peak to peak
	Gravity          float64       // g
	Still            bool          // enough reports with motion, and the controller lay still: the mean is the drift
	Saved            bool          // and it was saved
}

// GyroFit is how the mouse followed the controller in one phase. NaN: not
// measured.
type GyroFit struct {
	Counts  int64                       // mouse counts from this source, x and y together
	Turned  float64                     // degrees the controller turned
	Lag     time.Duration               // the mouse moves this long after EDSense reads the report
	Windows int                         // 100 ms windows faster than 10 deg/s, the ones fitted
	Side    float64                     // counts per degree sideways
	Roll    float64                     // share of roll added to sideways
	Up      float64                     // counts per degree up and down
	Leak    float64                     // counts per degree of pitch left in the sideways moves; expect 0
	Low     [len(GyroBands) + 1]float64 // share of the movement delivered, per band of GyroBands
}

// GyroResult is everything -gyrotest prints. AnalyseGyro fills the fits and
// the phase summaries; the runner fills Rest, Want and the Elite fields.
type GyroResult struct {
	Rest GyroRest
	DSX  GyroFit    // phase 2, DSX's moves
	Own  GyroFit    // phase 3, EDSense's moves
	Want [2]float64 // counts per degree EDSense's settings ask for; 0: not compared

	DSXEmpty   int // empty reports while DSX aimed: its motion to mouse passes none on
	DSXReports int

	OwnDSX     int64 // DSX's counts while EDSense aimed; expect 0, NONE stops its mouse
	OwnEmpty   int   // empty reports while EDSense aimed; expect 0, NONE passes the motion
	OwnReports int

	DriftSaved bool // the rest was not still, so the saved calibration's drift was taken off

	Touched    int   // reports with a finger on the touchpad
	TouchEmpty int   // of those, the empty ones: DSX's touch pause
	Lifts      int   // the finger left the touchpad
	LiftDSX    int64 // DSX's counts after the lifts, each until the next touch

	Device int64 // counts from a real mouse

	EliteRuns, EliteBlocked bool

	// the DSX profile measured (the one DSX used last) and the one it uses
	// for Elite; "" when unknown
	DSXProfile, EliteProfile string
}

// GyroBands are the upper edges of the low-speed bands, deg/s of sideways
// turn; the last band has no upper edge.
var GyroBands = [...]float64{2, 5, 10, 30}

const (
	gyroBin     = 10 * time.Millisecond
	gyroWindow  = 10 // bins: 100 ms
	lagMin      = -40 * time.Millisecond
	lagMax      = 80 * time.Millisecond
	fitRate     = 10.0                   // deg/s: slower windows lose counts to DSX's low-speed handling
	fitMin      = 100.0                  // degrees squared behind a gain: 16 windows at 25 deg/s
	lowMin      = 20.0                   // counts a low-speed band needs
	gyroSettle  = 250 * time.Millisecond // DSX's mouse may still move while it takes NONE
	movedDevice = 50                     // counts from a real mouse that spoil the test
	stillTurn   = 30.0                   // degrees: turned less, the controller hardly moved
)

func dsxMove(m GyroMouse) bool { return m.Injected && !m.Ours }
func ownMove(m GyroMouse) bool { return m.Injected && m.Ours }

func moveCounts(m GyroMouse) int64 { return abs64(int64(m.DX)) + abs64(int64(m.DY)) }

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// AnalyseGyro measures DSX's gyro in phase 2 and EDSense's in phase 3, and
// what DSX does while EDSense aims and around touches.
func AnalyseGyro(rec GyroRecording) GyroResult {
	r := GyroResult{
		DSX: fitPhase(rec, rec.DSX, dsxMove),
		Own: fitPhase(rec, rec.Own, ownMove),
	}
	aiming := GyroSpan{rec.Own.From.Add(gyroSettle), rec.Own.To}
	// touch reports are left out: DSX's touch pause empties them anyway
	for _, m := range rec.Motion {
		if aiming.has(m.At) && !m.Touch {
			r.OwnReports++
			if m.Empty {
				r.OwnEmpty++
			}
		}
		if rec.DSX.has(m.At) && !m.Touch {
			r.DSXReports++
			if m.Empty {
				r.DSXEmpty++
			}
		}
	}
	for _, m := range rec.Mouse {
		switch {
		case !m.Injected:
			r.Device += moveCounts(m)
		case dsxMove(m) && aiming.has(m.At):
			r.OwnDSX += moveCounts(m)
		}
	}
	r.touches(rec)
	return r
}

// touches sums up phase 4: whether DSX zeroes the motion while a finger
// rests on the touchpad, and whether its mouse comes back after a lift.
func (r *GyroResult) touches(rec GyroRecording) {
	var after []GyroSpan // from each lift to the next touch
	was := false
	for _, m := range rec.Motion {
		if !rec.Touch.has(m.At) {
			continue
		}
		switch {
		case m.Touch && !was && len(after) > 0:
			after[len(after)-1].To = m.At
		case !m.Touch && was:
			r.Lifts++
			after = append(after, GyroSpan{m.At, rec.Touch.To})
		}
		if m.Touch {
			r.Touched++
			if m.Empty {
				r.TouchEmpty++
			}
		}
		was = m.Touch
	}
	for _, m := range rec.Mouse {
		if !dsxMove(m) {
			continue
		}
		for _, s := range after {
			if s.has(m.At) {
				r.LiftDSX += moveCounts(m)
				break
			}
		}
	}
}

// bins is one phase's motion in 10 ms bins, by arrival time. Each report
// is spread over the time it covers, and each mouse move over one report
// period. Dropped whole into 10 ms bins, reports 4 ms apart fall two or
// three to a bin, and the lag search then follows that pattern.
type bins struct {
	from   time.Time
	period time.Duration // mean time a report covers
	deg    [][3]float64  // degrees turned: pitch, yaw, roll
	path   []float64     // degrees along the way, for the mean rate
	secs   []float64     // report time
	bad    []bool        // a touch or an empty report: the mouse need not follow
}

func binMotion(motion []GyroMotion, span GyroSpan) bins {
	n := max(0, int(span.To.Sub(span.From)/gyroBin))
	b := bins{from: span.From, period: 4 * time.Millisecond, deg: make([][3]float64, n),
		path: make([]float64, n), secs: make([]float64, n), bad: make([]bool, n)}
	var sum float64
	var reports int
	for _, m := range motion {
		if !span.has(m.At) {
			continue
		}
		if m.Touch || m.Empty {
			if i := int(m.At.Sub(span.From) / gyroBin); i < n {
				b.bad[i] = true
			}
			continue
		}
		path := math.Sqrt(m.Rate[0]*m.Rate[0]+m.Rate[1]*m.Rate[1]+m.Rate[2]*m.Rate[2]) * m.Dt
		b.shares(m.At.Add(-time.Duration(m.Dt*float64(time.Second))), m.At, func(i int, share float64) {
			for k, w := range m.Rate {
				b.deg[i][k] += w * m.Dt * share
			}
			b.path[i] += path * share
			b.secs[i] += m.Dt * share
		})
		if m.Dt > 0 {
			sum += m.Dt
			reports++
		}
	}
	if reports > 0 {
		b.period = time.Duration(sum / float64(reports) * float64(time.Second))
	}
	return b
}

// shares calls f with each bin that t0..t1 overlaps and the share of that
// time in it; all in one bin when t1 is not after t0.
func (b *bins) shares(t0, t1 time.Time, f func(i int, share float64)) {
	x0 := float64(t0.Sub(b.from)) / float64(gyroBin)
	x1 := float64(t1.Sub(b.from)) / float64(gyroBin)
	n := len(b.deg)
	if x1 <= x0 {
		if i := int(math.Floor(x0)); i >= 0 && i < n {
			f(i, 1)
		}
		return
	}
	if x1 <= 0 || x0 >= float64(n) {
		return
	}
	for i := max(0, int(math.Floor(x0))); i < n && float64(i) < x1; i++ {
		f(i, (math.Min(x1, float64(i+1))-math.Max(x0, float64(i)))/(x1-x0))
	}
}

// counts sums the kept mouse moves into the bins, each taken lag earlier.
func (b *bins) counts(moves []GyroMouse, keep func(GyroMouse) bool, lag time.Duration) (dx, dy []float64) {
	dx, dy = make([]float64, len(b.deg)), make([]float64, len(b.deg))
	for _, m := range moves {
		if !keep(m) {
			continue
		}
		at := m.At.Add(-lag)
		b.shares(at.Add(-b.period), at, func(i int, share float64) {
			dx[i] += float64(m.DX) * share
			dy[i] += float64(m.DY) * share
		})
	}
	return dx, dy
}

// lag is the shift at which the motion explains the mouse moves best,
// sideways and up-down together, tried in 1 ms steps. Smooth hand motion
// gives a broad peak, and timing jitter makes its top ragged, so the answer
// is the top of a parabola fitted over 20 ms either side of the best step.
func (b *bins) lag(moves []GyroMouse, keep func(GyroMouse) bool) time.Duration {
	var fit []float64
	for s := lagMin; s <= lagMax; s += time.Millisecond {
		dx, dy := b.counts(moves, keep, s)
		var sum sums
		for i, d := range b.deg {
			sum.add(d, dx[i], dy[i])
		}
		fit = append(fit, sum.explained())
	}
	best := 0
	for i := range fit {
		if fit[i] > fit[best] {
			best = i
		}
	}
	if fit[best] == 0 {
		return 0
	}
	peak := float64(best) + vertex(fit, best, 20)
	return lagMin + time.Duration(peak*float64(time.Millisecond))
}

// vertex is where a parabola fitted to y[i-h..i+h] peaks, relative to i;
// 0 when there is too little room either side or y does not curve down.
func vertex(y []float64, i, h int) float64 {
	h = min(h, i, len(y)-1-i)
	if h < 2 {
		return 0
	}
	var n, x2, x4, sy, xy, x2y float64
	for j := -h; j <= h; j++ {
		x, v := float64(j), y[i+j]
		n++
		x2 += x * x
		x4 += x * x * x * x
		sy += v
		xy += x * v
		x2y += x * x * v
	}
	curve := (n*x2y - x2*sy) / (n*x4 - x2*x2)
	if curve >= 0 {
		return 0
	}
	return math.Max(-float64(h), math.Min(float64(h), -xy/x2/(2*curve)))
}

// sums holds the sums of products the least squares fits need: yaw, roll
// and pitch in degrees against the mouse's x and y counts (xx and dd are
// dx and dy squared).
type sums struct{ yy, yr, rr, pp, yx, rx, py, xx, dd float64 }

func (s *sums) add(deg [3]float64, dx, dy float64) {
	p, y, r := deg[0], deg[1], deg[2]
	s.yy += y * y
	s.yr += y * r
	s.rr += r * r
	s.pp += p * p
	s.yx += y * dx
	s.rx += r * dx
	s.py += p * dy
	s.xx += dx * dx
	s.dd += dy * dy
}

// sideways fits dx = a*yaw + b*roll by least squares without intercept.
// Roll is left out (b 0, false) when it did not vary apart from yaw by
// minRoll degrees squared.
func (s *sums) sideways(minRoll float64) (a, b float64, roll bool) {
	if det := s.yy*s.rr - s.yr*s.yr; s.rr > 0 && det/s.yy >= minRoll {
		return (s.yx*s.rr - s.rx*s.yr) / det, (s.rx*s.yy - s.yx*s.yr) / det, true
	}
	return s.yx / s.yy, 0, false
}

// explained is the share of the mouse counts' spread that the motion
// explains through the fits; 0 with no counts.
func (s *sums) explained() float64 {
	if s.xx+s.dd == 0 {
		return 0
	}
	var e float64
	if s.yy > 0 {
		a, b, _ := s.sideways(1)
		e += a*s.yx + b*s.rx
	}
	if s.pp > 0 {
		e += s.py * s.py / s.pp
	}
	return e / (s.xx + s.dd)
}

// window is 100 ms of a phase: the degrees turned and the mouse counts.
type window struct {
	deg        [3]float64
	dx, dy     float64
	secs, path float64
}

func fitPhase(rec GyroRecording, span GyroSpan, keep func(GyroMouse) bool) GyroFit {
	f := GyroFit{Side: math.NaN(), Roll: math.NaN(), Up: math.NaN(), Leak: math.NaN()}
	for i := range f.Low {
		f.Low[i] = math.NaN()
	}
	for _, m := range rec.Mouse {
		if keep(m) && span.has(m.At) {
			f.Counts += moveCounts(m)
		}
	}
	b := binMotion(rec.Motion, span)
	for _, p := range b.path {
		f.Turned += p
	}
	if f.Counts == 0 || len(b.deg) < gyroWindow {
		return f
	}
	f.Lag = b.lag(rec.Mouse, keep)
	dx, dy := b.counts(rec.Mouse, keep, f.Lag)
	var ws []window
	for i := 0; i+gyroWindow <= len(b.deg); i += gyroWindow {
		if w, ok := b.window(i, dx, dy); ok {
			ws = append(ws, w)
		}
	}
	f.fit(ws)
	return f
}

// window sums the 10 bins from i; false when a bin is bad or reports are
// missing for half the time.
func (b *bins) window(i int, dx, dy []float64) (window, bool) {
	var w window
	for j := i; j < i+gyroWindow; j++ {
		if b.bad[j] {
			return w, false
		}
		for k := range w.deg {
			w.deg[k] += b.deg[j][k]
		}
		w.dx += dx[j]
		w.dy += dy[j]
		w.secs += b.secs[j]
		w.path += b.path[j]
	}
	return w, w.secs >= (gyroWindow*gyroBin).Seconds()/2
}

// fit finds the gains by least squares without intercept over the fast
// windows: dx = a*yaw + b*roll, dy = c*pitch, all in degrees. The signs
// follow EDSense's formula (turning left moves the mouse left, tilting up
// moves it up), so DSX's gains come out positive when it agrees.
func (f *GyroFit) fit(ws []window) {
	var sum sums
	fast := func(w window) bool { return w.path/w.secs > fitRate }
	for _, w := range ws {
		if fast(w) {
			f.Windows++
			sum.add(w.deg, w.dx, w.dy)
		}
	}
	if sum.pp >= fitMin {
		f.Up = -sum.py / sum.pp
	}
	if sum.yy < fitMin {
		return
	}
	a, b, roll := sum.sideways(fitMin)
	if a == 0 {
		return
	}
	f.Side = -a
	if roll {
		f.Roll = b / a
	}
	// the check: pitch should explain nothing of what the sideways fit leaves
	if sum.pp >= fitMin {
		var pr float64
		for _, w := range ws {
			if fast(w) {
				pr += w.deg[0] * (w.dx - a*w.deg[1] - b*w.deg[2])
			}
		}
		f.Leak = pr / sum.pp
	}
	// low speed: counts delivered over counts the gains predict, per band
	// of the window's sideways rate, over every window
	var got, want [len(GyroBands) + 1]float64
	for _, w := range ws {
		pred := a*w.deg[1] + b*w.deg[2]
		if pred == 0 {
			continue
		}
		i := band(math.Abs(pred/a) / w.secs)
		if pred < 0 {
			got[i] -= w.dx
		} else {
			got[i] += w.dx
		}
		want[i] += math.Abs(pred)
	}
	for i := range want {
		if want[i] >= lowMin {
			f.Low[i] = got[i] / want[i]
		}
	}
}

func band(rate float64) int {
	for i, edge := range GyroBands {
		if rate < edge {
			return i
		}
	}
	return len(GyroBands)
}

// Print writes the result as -gyrotest shows it.
func (r GyroResult) Print(w io.Writer) {
	for _, line := range r.lines() {
		fmt.Fprintln(w, line)
	}
}

func (r GyroResult) lines() []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	rest := r.Rest
	if rest.Reports == 0 {
		add("Controller: no motion reports at rest. In DSX, set the controller to DualSense emulation.")
	} else {
		clock := "no sensor clock"
		if rest.ClockHz > 0 {
			clock = fmt.Sprintf("sensor clock %.2f MHz", rest.ClockHz/1e6)
		}
		add("Controller: %.1f reports/s, %s, %d repeats, %d empty", rest.PerSecond, clock, rest.Repeats, rest.Empty)
		add("Reports every %s (median), 95%% within %s, longest gap %s, clock steps of %s",
			millis(rest.Median), millis(rest.P95), millis(rest.Max), millis(rest.Step))
		noMotion := rest.Reports-rest.Empty <= 100
		var saved string
		switch {
		case rest.Saved:
			saved = "Calibration saved."
		case rest.Still:
			saved = "The calibration could not be saved."
		case noMotion:
			saved = "No calibration was saved."
		default:
			saved = "The controller moved, so no calibration was saved."
		}
		switch {
		case rest.Still:
		case r.DriftSaved:
			saved += " The saved calibration is used below."
		default:
			saved += " No drift is taken off below, so the slow bands may be off."
		}
		if noMotion {
			add("At rest: the reports carry no motion, so the DSX profile in use passes none to the virtual DualSense (Passthrough off on DSX's Motion page, or its motion off). %s", saved)
		} else {
			noise := math.Max(rest.Noise[0], math.Max(rest.Noise[1], rest.Noise[2]))
			add("At rest: drift %.2f %.2f %.2f deg/s, noise %.1f deg/s peak to peak, gravity %.2f g. %s",
				rest.Bias[0], rest.Bias[1], rest.Bias[2], noise, rest.Gravity, saved)
		}
	}

	d := r.DSX
	switch {
	// with no motion the controller seems not to turn, whatever the player did
	case r.DSXReports > 0 && r.DSXEmpty*2 >= r.DSXReports:
		add("DSX: its motion to mouse passes no motion to the virtual DualSense (%d of %d reports empty), so it cannot be measured. Turn on Passthrough on DSX's Motion page and run the test again.", r.DSXEmpty, r.DSXReports)
	case d.Counts == 0 && d.Turned < stillTurn:
		add("DSX: the controller hardly moved, so there is nothing to measure")
	case d.Counts == 0:
		add("DSX: no mouse movement. Is motion to mouse on in the DSX profile?")
	case math.IsNaN(d.Side) && math.IsNaN(d.Up):
		add("DSX: too little fast movement to measure; turn the controller faster")
	default:
		add("DSX: %s; mouse %s", d.gains(), lagText(d.Lag))
		if !math.IsNaN(d.Side) { // the bands need the sideways gain
			add("DSX at low speed: %s", d.lowText())
		}
	}
	if math.Abs(d.Leak) >= 1 {
		add("DSX also moves the mouse sideways by %.1f counts per degree of tilt (expected 0)", d.Leak)
	}
	if d.Side < 0 {
		add("DSX moves the mouse sideways the other way from EDSense, so EDSense's gyro would turn the wrong way")
	}
	if d.Up < 0 {
		add("DSX moves the mouse up and down the other way from EDSense, so EDSense's gyro would tilt the wrong way")
	}
	// Roll is a share of the sideways gain, so its sign is relative:
	// inverting DSX's sideways flips yaw and roll together and leaves it
	// positive.
	if d.Roll*d.Side < 0 {
		add("Rolling moves DSX's mouse the other way from EDSense's")
	}

	o := r.Own
	own := "EDSense: " + o.gains()
	switch {
	case r.OwnReports == 0:
		own = "EDSense: no motion reports arrived while it aimed"
	case r.OwnEmpty*2 >= r.OwnReports:
		own = "EDSense: DSX passes no motion while its mouse is off, so EDSense's gyro has nothing to aim with"
	case o.Counts == 0 && o.Turned < stillTurn:
		own = "EDSense: the controller hardly moved"
	case o.Counts == 0:
		own = "EDSense: no mouse movement"
	case math.IsNaN(o.Side) && math.IsNaN(o.Up):
		own = "EDSense: too little fast movement to measure"
	case r.Want[0] > 0 && r.Want[1] > 0 && (off(o.Side, r.Want[0]) || off(o.Up, r.Want[1])):
		own += fmt.Sprintf(" (the settings ask for %.1f and %.1f)", r.Want[0], r.Want[1])
	}
	dsx := "its mouse is off"
	if r.OwnDSX > 0 {
		dsx = "its mouse is still on, so both move the mouse"
	}
	empty := fmt.Sprintf(", %d empty reports", r.OwnEmpty)
	switch {
	case r.OwnReports == 0:
		empty = ""
	case r.OwnEmpty > 0:
		empty += fmt.Sprintf(" of %d: DSX zeroes the motion", r.OwnReports)
	}
	add("%s. DSX meanwhile: %d counts (%s)%s", own, r.OwnDSX, dsx, empty)

	add("Touch: %s", r.touchText())

	if r.EliteRuns {
		if r.EliteBlocked {
			add("Elite: runs as administrator, so EDSense's gyro cannot reach it. Start Elite normally, or run EDSense as administrator too.")
		} else {
			add("Elite: runs as your user, EDSense's gyro can reach it")
		}
	}
	if r.Device > movedDevice {
		add("The mouse was moved during the test (%d counts), so the numbers may be off.", r.Device)
	}
	otherProfile := r.DSXProfile != "" && r.EliteProfile != "" && r.DSXProfile != r.EliteProfile
	if d.Counts > 0 && d.Side > 0 && otherProfile {
		add("DSX's numbers are for its profile %q; in Elite it uses %q, so nothing is suggested. Select %q in DSX and run the test again to compare.", r.DSXProfile, r.EliteProfile, r.EliteProfile)
	}
	if d.Counts > 0 && d.Side > 0 && !otherProfile {
		keys := []string{fmt.Sprintf(`"gyro_sensitivity_x": %.2f`, d.Side/gyro.DSXCountsPerDegree)}
		if d.Up > 0 {
			keys = append(keys, fmt.Sprintf(`"gyro_sensitivity_y": %.2f`, d.Up/gyro.DSXCountsPerDegree))
		}
		if d.Roll >= 0 {
			keys = append(keys, fmt.Sprintf(`"gyro_roll_mix": %.2f`, d.Roll))
		}
		add("Suggested for edsense.json: %s", strings.Join(keys, ", "))
	}
	return out
}

// touchText: "DSX pauses the motion while touching; after lifting DSX moved
// the mouse 0 counts".
func (r GyroResult) touchText() string {
	var touch string
	switch {
	case r.Touched == 0:
		return "no touch seen"
	case r.TouchEmpty*10 >= r.Touched*9:
		touch = "DSX pauses the motion while touching"
	case r.TouchEmpty == 0:
		touch = "the motion keeps coming while touching"
	default:
		touch = fmt.Sprintf("%d of %d reports empty while touching", r.TouchEmpty, r.Touched)
	}
	switch {
	case r.Lifts == 0:
		return touch + "; no lift seen"
	case r.LiftDSX == 0:
		return touch + "; after lifting DSX moved the mouse 0 counts"
	}
	return touch + fmt.Sprintf("; after lifting DSX moved the mouse %d counts (its motion to mouse came back)", r.LiftDSX)
}

// gains: "22.4 counts/deg sideways (roll adds 0.61 of that), 22.5 up and down".
func (f GyroFit) gains() string {
	side := "sideways not measured"
	if !math.IsNaN(f.Side) {
		side = fmt.Sprintf("%.1f counts/deg sideways", f.Side)
		if math.IsNaN(f.Roll) {
			side += " (roll not measured)"
		} else {
			side += fmt.Sprintf(" (roll adds %.2f of that)", f.Roll)
		}
	}
	up := "up and down not measured"
	if !math.IsNaN(f.Up) {
		up = fmt.Sprintf("%.1f up and down", f.Up)
	}
	return side + ", " + up
}

// lowText: "under 2 deg/s 12%, 2-5 88%, 5-10 95%, 10-30 98%, over 30 99%".
func (f GyroFit) lowText() string {
	parts := make([]string, len(f.Low))
	for i, share := range f.Low {
		switch {
		case i == 0:
			parts[i] = fmt.Sprintf("under %g deg/s", GyroBands[0])
		case i == len(GyroBands):
			parts[i] = fmt.Sprintf("over %g", GyroBands[i-1])
		default:
			parts[i] = fmt.Sprintf("%g-%g", GyroBands[i-1], GyroBands[i])
		}
		if math.IsNaN(share) {
			parts[i] += " -"
		} else {
			parts[i] += fmt.Sprintf(" %.0f%%", 100*share)
		}
	}
	return strings.Join(parts, ", ")
}

func lagText(d time.Duration) string {
	ms := math.Round(d.Seconds() * 1000)
	if ms < 0 {
		return fmt.Sprintf("%.0f ms before the report", -ms)
	}
	return fmt.Sprintf("%.0f ms after the report", ms)
}

func millis(d time.Duration) string { return fmt.Sprintf("%.1f ms", d.Seconds()*1000) }

// off: a measured gain more than 3% from the one asked for.
func off(got, want float64) bool { return !math.IsNaN(got) && math.Abs(got/want-1) > 0.03 }
