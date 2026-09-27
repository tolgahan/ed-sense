package hud

import (
	"log"
	"time"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// learner looks for the HUD colours on screen: once per session to check
// them (with the wrong colours the reader can lock onto the wrong text and
// never notice), and whenever the shield % goes unread for a while with
// shields up. Two matching results in a row are adopted.
type learner struct {
	lastSeen   time.Time     // last learnColours call
	lastTry    time.Time     // last look
	noShield   time.Duration // shields up but no shield % read
	upTime     time.Duration // reading with shields up
	found      []Calibration
	misses     int
	verified   bool // the colours were checked on screen
	checkTries int
}

// restart: new colours were set.
func (l *learner) restart() {
	l.found, l.noShield, l.upTime, l.verified, l.checkTries = nil, 0, 0, false, 0
}

func (w *Watcher) learnColours(now time.Time, r Reading, cw, ch int) {
	l := &w.learner
	w.mu.Lock()
	up, learn, pal := w.shieldsUp, w.learn, w.pal
	if w.newPalette {
		l.restart()
		w.newPalette = false
	}
	w.mu.Unlock()
	// long gaps between calls mean reading was off (menus, docked)
	dt := now.Sub(l.lastSeen)
	l.lastSeen = now
	if dt > 2*time.Second {
		dt = 0
	}
	confident := r.Shield.Found() && r.Shield.Score >= 0.7
	if confident {
		l.noShield = 0
	}
	if !up || !learn {
		return
	}
	l.upTime += dt
	if !confident {
		l.noShield += dt
	}
	check := !l.verified && l.checkTries < 6 && l.upTime >= 5*time.Second
	if !check && (confident || l.noShield < 30*time.Second) {
		return
	}
	gap := 20 * time.Second
	if check || len(l.found) > 0 {
		gap = 5 * time.Second
	}
	if now.Sub(l.lastTry) < gap {
		return
	}
	l.lastTry = now
	img, err := w.grab.Grab(shieldArea(cw, ch))
	if err != nil {
		return
	}
	start := time.Now()
	c := calibrate(img, vision.Rect{X1: img.W, Y1: img.H}, ch)
	if !c.OK {
		if check {
			l.checkTries++
			return
		}
		// the external camera, looking around and the like hide the panel too
		if l.misses%15 == 0 {
			log.Printf("HUD: shield %% not found for a while; looked for your HUD colours, nothing found yet (%d ms)", time.Since(start).Milliseconds())
		}
		l.misses++
		return
	}
	l.misses = 0
	if check && vision.ColorDistance(c.Shield, pal.Shield) < 0.2 {
		l.verified, l.found = true, nil
		log.Printf("HUD: colours checked on screen, they match (shield %s, %d ms)", c.Shield.Hex(), time.Since(start).Milliseconds())
		return
	}
	c.Heat = c.Main
	if heat, err := w.grab.Grab(heatArea(cw, ch)); err == nil {
		base := pal
		base.Shield = c.Shield
		c.Heat, _ = calibrateHeat(heat, vision.Rect{X1: heat.W, Y1: heat.H}, ch, c.Main, base)
	}
	log.Printf("HUD: colours seen: shield %s (reads %s), HUD %s (hull %s), heat %s (%d ms)", c.Shield.Hex(), c.ShieldText, c.Main.Hex(), c.HullText, c.Heat.Hex(), time.Since(start).Milliseconds())
	l.found = append(l.found, c)
	if n := len(l.found); n < 2 || !l.found[n-1].Agrees(l.found[n-2]) {
		return
	}
	w.mu.Lock()
	p := w.pal.WithCalibration(c)
	w.pal = p
	onLearned := w.onLearned
	w.mu.Unlock()
	log.Printf("HUD: using the colours found on screen (shield %s, heat %s)", p.Shield.Hex(), p.Heat.Hex())
	l.found, l.noShield, l.verified = nil, 0, true
	if onLearned != nil {
		onLearned(p)
	}
}
