package hud

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// Grabber captures parts of the game window, in client coordinates.
type Grabber interface {
	// Window reports the game's client size while it is visible and in front.
	Window() (w, h int, ok bool)
	Grab(r vision.Rect) (*vision.Image, error)
}

// Watcher reads the HUD in the background while the player is in the
// cockpit. It follows what it found with small capture windows, and scans
// the whole area only when it has lost it.
type Watcher struct {
	grab Grabber

	mu          sync.Mutex // guards the fields below, up to the blank line
	proc        *processor
	pal         Palette
	learn       bool
	onLearned   func(Palette)
	newPalette  bool
	active      bool
	dropWindows bool
	shieldsUp   bool
	fast        bool // combat or close to it: read 10 times a second, else 4
	debugDir    string
	listsOn     bool         // in the cockpit with the lists in view
	firing      [2]time.Time // trigger last held, by list
	listDue     [2]bool      // read soon: the fire group changed

	n                  int // reads, to spread out the slower ones
	shieldWin, heatWin *vision.Rect
	shieldGH           float64 // glyph height when the shield window was set; 0: search all of it
	targetWin          *vision.Rect
	target             Target // where the target panel was
	targetMisses       int
	lastTargetScan     time.Time
	lastBlueZone       time.Time
	lastFull           time.Time
	lostSince          time.Time
	lastTick           time.Time
	lastDump           time.Time
	lists              [2]listWatch
	lastListScan       time.Time
	learner            learner
	stats              readStats
	shots              []debugShot // this tick's captures, for debugging
}

type readStats struct {
	grab, read, active time.Duration
	reads, fullReads   int
	nextLog            int
}

func NewWatcher(g Grabber) *Watcher {
	return &Watcher{grab: g, proc: newProcessor(), pal: DefaultPalette}
}

// SetPalette sets the HUD colours to look for. With learn set, the watcher
// looks for the colours on screen when it cannot read the shield % for a
// while, and calls onLearned with what it found.
func (w *Watcher) SetPalette(p Palette, learn bool, onLearned func(Palette)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pal, w.learn, w.onLearned, w.newPalette = p, learn, onLearned, true
}

// SetActive turns reading on (in the cockpit, game in front) or off.
func (w *Watcher) SetActive(on, shieldsUp bool, debugDir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active && !on {
		w.proc.lost()
		w.dropWindows = true
	}
	w.active, w.shieldsUp, w.debugDir = on, shieldsUp, debugDir
}

// SetFast: read 10 times a second (combat, shields or heat changing) or 4.
func (w *Watcher) SetFast(fast bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fast = fast
}

// ResetTarget: the player picked another target.
func (w *Watcher) ResetTarget() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.proc.resetTarget()
}

// SetLoadout sets the ship's fire-groupable modules.
func (w *Watcher) SetLoadout(mods []elite.Module) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.proc.setModules(mods)
}

// SetFireGroups: whether the lists are worth reading (in the cockpit, not in
// analysis mode), which fire group and hardpoint state they show (FireKey),
// and which triggers are held, by list.
func (w *Watcher) SetFireGroups(on bool, key int, deployed bool, held [2]bool, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.listsOn = on
	if on && (!w.proc.fireLists.keySet || w.proc.fireLists.key != key) {
		w.proc.setFireKey(key, deployed, now)
		w.listDue = [2]bool{true, true}
	}
	for i, h := range held {
		if h {
			w.firing[i] = now
		}
	}
}

// Take returns the state and the events since the last call.
func (w *Watcher) Take() State {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.proc.take()
}

// Run reads until stop is closed.
func (w *Watcher) Run(stop <-chan struct{}) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		w.mu.Lock()
		on, fast := w.active, w.fast
		w.mu.Unlock()
		now := time.Now()
		if on && (fast || now.Sub(w.lastTick) >= 240*time.Millisecond) {
			w.tick(now)
		}
	}
}

func (w *Watcher) tick(now time.Time) {
	cw, ch, ok := w.grab.Window()
	w.mu.Lock()
	if !ok {
		w.proc.lost()
		w.mu.Unlock()
		return
	}
	pal, fast := w.pal, w.fast
	if w.dropWindows {
		w.shieldWin, w.heatWin, w.dropWindows = nil, nil, false
	}
	w.mu.Unlock()

	full := w.shieldWin == nil || w.heatWin == nil
	if full {
		if w.lostSince.IsZero() {
			w.lostSince = now
		}
		// searching: 3 full scans a second, 1 a second after a while
		gap := 300 * time.Millisecond
		if now.Sub(w.lostSince) > 3*time.Second {
			gap = time.Second
		}
		if now.Sub(w.lastFull) < gap {
			w.listsOnly(now, cw, ch, pal, fast) // the lists keep their own pace
			return
		}
		w.lastFull = now
		w.stats.fullReads++
	} else {
		w.lostSince = time.Time{}
	}
	if d := now.Sub(w.lastTick); !w.lastTick.IsZero() && d < 1100*time.Millisecond {
		w.stats.active += d
	}
	w.lastTick = now

	start, grabbed := time.Now(), w.stats.grab
	var r Reading
	w.shots = w.shots[:0]
	w.n++
	w.readShipPanel(&r, cw, ch, pal)
	w.readRadar(&r, now, cw, ch, pal)
	w.readTargetPanel(&r, now, cw, ch, pal, fast)
	w.follow(r, cw, ch)
	if list, rd, img, ok := w.readList(now, cw, ch, pal, fast); ok {
		r.Lists[list] = rd
		w.shot(fmt.Sprintf("%s-%d", [2]string{"weaponsL", "weaponsR"}[list], len(rd.Entries)), img)
	}
	w.stats.read += time.Since(start) - (w.stats.grab - grabbed)
	w.stats.reads++

	w.learnColours(now, r, cw, ch)
	w.mu.Lock()
	w.proc.feed(r, now)
	st, dir := w.proc.state, w.debugDir
	w.mu.Unlock()

	w.logStats(st)
	if dir != "" && now.Sub(w.lastDump) > 3*time.Second {
		w.lastDump = now
		saveDebug(dir, now, r, w.shots)
	}
}

// capture grabs r and counts the time it took.
func (w *Watcher) capture(r vision.Rect) (*vision.Image, error) {
	start := time.Now()
	img, err := w.grab.Grab(r)
	w.stats.grab += time.Since(start)
	return img, err
}

// readShipPanel reads the right-hand panel: the shield %, and now and then
// the hull % and the capacitors.
func (w *Watcher) readShipPanel(r *Reading, cw, ch int, pal Palette) {
	area := shieldArea(cw, ch)
	if w.shieldWin != nil {
		area = *w.shieldWin
	}
	img, err := w.capture(area)
	if err != nil {
		return
	}
	w.shot("ship", img)
	// the window also holds the hull % and the capacitors; the shield % is
	// where it was
	in := vision.Rect{X1: img.W, Y1: img.H}
	if w.shieldWin != nil && w.shieldGH > 0 {
		g := w.shieldGH
		in = vision.Rect{X0: int(4 * g), X1: int(32 * g), Y1: int(16 * g)}.Clip(img.W, img.H)
	}
	r.Shield, r.Splash, r.SplashPan = readShield(img, in, ch, pal)
	if !r.Shield.Found() {
		return
	}
	x, y := r.Shield.Center()
	if w.n%3 == 0 {
		r.Hull = readHull(img, x, y, r.Shield.GH, ch, pal)
		r.Hull.Box = r.Hull.Box.Offset(area.X0, area.Y0)
	}
	if w.n%2 == 0 {
		r.Caps, r.CapsOK = readCapacitors(img, x, y, r.Shield.GH, pal)
	}
	r.Shield.Box = r.Shield.Box.Offset(area.X0, area.Y0)
}

// readRadar reads the heat % and, beside it, the throttle arc.
func (w *Watcher) readRadar(r *Reading, now time.Time, cw, ch int, pal Palette) {
	area := heatArea(cw, ch)
	if w.heatWin != nil {
		area = *w.heatWin
	}
	if img, err := w.capture(area); err == nil {
		w.shot("heat", img)
		if r.Heat = readHeat(img, vision.Rect{X1: img.W, Y1: img.H}, ch, pal); r.Heat.Found() {
			r.Heat.Box = r.Heat.Box.Offset(area.X0, area.Y0)
		}
	}
	if !r.Heat.Found() || r.Heat.Score < 0.6 || now.Sub(w.lastBlueZone) < 200*time.Millisecond {
		return
	}
	w.lastBlueZone = now
	x, y := r.Heat.Center()
	zone := blueZoneBox.around(x, y, r.Heat.GH, 0, 0).Clip(cw, ch)
	if img, err := w.capture(zone); err == nil {
		w.shot("throttle", img)
		r.BlueZoneSeen, r.InBlueZone = readBlueZone(img, x-float64(zone.X0), y-float64(zone.Y0), r.Heat.GH)
	}
}

// readTargetPanel reads the target panel where it was found, and looks for
// it once a second in combat.
func (w *Watcher) readTargetPanel(r *Reading, now time.Time, cw, ch int, pal Palette, combat bool) {
	tracked := w.targetWin != nil
	due := tracked && w.n%2 == 1 || !tracked && combat && now.Sub(w.lastTargetScan) >= time.Second
	if !due {
		return
	}
	area := targetArea(cw, ch)
	var last *Target
	if tracked {
		area = *w.targetWin
		last = &Target{X: w.target.X - float64(area.X0), Y: w.target.Y - float64(area.Y0), GH: w.target.GH}
	} else {
		w.lastTargetScan = now
	}
	img, err := w.capture(area)
	if err != nil {
		return
	}
	w.shot("target", img)
	t := readTarget(img, vision.Rect{X1: img.W, Y1: img.H}, ch, pal, last)
	if t.Found() {
		t.X, t.Y = t.X+float64(area.X0), t.Y+float64(area.Y0)
		t.Shield.Box = t.Shield.Box.Offset(area.X0, area.Y0)
		t.Hull.Box = t.Hull.Box.Offset(area.X0, area.Y0)
	}
	r.Target = t
}

func (w *Watcher) shot(name string, img *vision.Image) {
	w.shots = append(w.shots, debugShot{name, img})
}

// follow sets the capture windows around what was read confidently, and
// drops them when it was not.
func (w *Watcher) follow(r Reading, cw, ch int) {
	w.shieldWin, w.shieldGH = nil, 0
	if r.Shield.Found() && r.Shield.Score >= 0.55 {
		x, y := r.Shield.Center()
		g := r.Shield.GH
		// the window's left and top edges must stay put relative to the text
		// (the shield % is searched at a fixed place in it), so it is clipped
		// after checking them
		win := vision.Rect{X0: int(x - 18*g), Y0: int(y - 12*g), X1: int(x + 27*g), Y1: int(y + 10*g)}
		w.shieldGH = g
		if win.X0 < 0 || win.Y0 < 0 {
			w.shieldGH = 0 // at the screen edge: search the whole window
		}
		win = win.Clip(cw, ch)
		w.shieldWin = &win
	}
	w.heatWin = nil
	if r.Heat.Found() && r.Heat.Score >= 0.55 {
		x, y := r.Heat.Center()
		g := r.Heat.GH
		win := vision.Rect{X0: int(x - 6*g), Y0: int(y - 3*g), X1: int(x + 5*g), Y1: int(y + 3*g)}.Clip(cw, ch)
		w.heatWin = &win
	}
	switch {
	case r.Target.Found():
		t := r.Target
		w.target = t
		win := vision.Rect{X0: int(t.X - 14*t.GH), Y0: int(t.Y - 13*t.GH), X1: int(t.X + 16*t.GH), Y1: int(t.Y + 11*t.GH)}.Clip(cw, ch)
		w.targetWin, w.targetMisses = &win, 0
	case w.targetWin != nil && w.n%2 == 1:
		if w.targetMisses++; w.targetMisses > 6 {
			w.targetWin = nil
		}
	}
}

// logStats logs what reading costs: after 300 reads, then every 5000 (about
// 15-20 minutes).
func (w *Watcher) logStats(st State) {
	s := &w.stats
	if s.reads < 300 || s.reads < s.nextLog {
		return
	}
	s.nextLog = s.reads + 5000
	n := float64(s.reads)
	grabMs := float64(s.grab.Microseconds()) / 1000 / n
	readMs := float64(s.read.Microseconds()) / 1000 / n
	perSec := n / math.Max(1, s.active.Seconds())
	log.Printf("HUD: %d reads (%.1f/s, %d full scans), capture %.1f ms + reading %.1f ms each, about %.0f%% of one CPU core; shield text found in %d (shield %v %d%%, heat %v %d%%)",
		s.reads, perSec, s.fullReads, grabMs, readMs, (grabMs+readMs)*perSec/10, st.ShieldReads, st.Shield.OK, st.Shield.Value, st.Heat.OK, st.Heat.Value)
}
