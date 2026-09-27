package hud

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// listWatch follows one fire group list.
type listWatch struct {
	win      *vision.Rect // where the list was found
	misses   int          // tracked reads in a row without it
	last     time.Time    // last read
	lastFull time.Time    // last search of the whole area
}

// listsOnly is a tick while the ship panel is being searched for (about
// once a second): the lists are still read at their own pace.
func (w *Watcher) listsOnly(now time.Time, cw, ch int, pal Palette, combat bool) {
	start, grabbed := time.Now(), w.stats.grab
	list, rd, _, ok := w.readList(now, cw, ch, pal, combat)
	if !ok {
		return
	}
	w.stats.read += time.Since(start) - (w.stats.grab - grabbed)
	w.stats.reads++
	w.mu.Lock()
	w.proc.feedList(list, rd, now)
	w.mu.Unlock()
}

// readList reads one list if one is due (see nextList).
func (w *Watcher) readList(now time.Time, cw, ch int, pal Palette, combat bool) (int, ListRead, *vision.Image, bool) {
	w.mu.Lock()
	on := w.listsOn
	w.mu.Unlock()
	if !on {
		return 0, ListRead{}, nil, false
	}
	list, full, ok := w.nextList(now, combat)
	if !ok {
		return 0, ListRead{}, nil, false
	}
	lw := &w.lists[list]
	lw.last = now
	if full {
		lw.lastFull, w.lastListScan = now, now
	}
	right := list == Primary
	area := listArea(cw, ch, right)
	if !full {
		area = *lw.win
	}
	img, err := w.capture(area)
	if err != nil {
		return 0, ListRead{}, nil, false
	}
	rd := readList(img, vision.Rect{X1: img.W, Y1: img.H}, ch, pal, right)
	for i := range rd.Entries {
		e := &rd.Entries[i]
		e.X0, e.X1 = e.X0+float64(area.X0), e.X1+float64(area.X0)
		e.Y0, e.Y1 = e.Y0+float64(area.Y0), e.Y1+float64(area.Y0)
	}
	w.trackList(list, rd, cw, ch)
	return list, rd, img, true
}

// nextList picks the list to read this tick, if any: the one being fired
// from (4 times a second, every tick while a weapon reloads), else each once
// a second where it was, and a search of the whole area every few seconds
// while it is lost (one list at a time: the area is big).
func (w *Watcher) nextList(now time.Time, combat bool) (list int, full, ok bool) {
	w.mu.Lock()
	firing, due := w.firing, w.listDue
	state := w.proc.state.Lists
	w.mu.Unlock()
	best, bestLate := -1, 0.0
	for i := range w.lists {
		firingNow := now.Sub(firing[i]) < 1500*time.Millisecond
		var every time.Duration
		switch st := state[i]; {
		case w.lists[i].win == nil:
			every = 3 * time.Second
			if combat {
				every = 2 * time.Second
			}
			if due[i] || firingNow {
				every = 500 * time.Millisecond
			}
		case st.Reloading > 0 && now.Sub(st.At) < 3*time.Second:
			every = 0
		case firingNow && st.Ammo > 0:
			every = 250 * time.Millisecond
		case due[i]:
			every = 0
		case !st.Known:
			every = 300 * time.Millisecond // until this fire group's list is known
		default:
			every = time.Second
		}
		late := now.Sub(w.lists[i].last).Seconds() - every.Seconds()
		if late < 0 {
			continue
		}
		// searches: one at a time, spaced out
		if w.lists[i].win == nil && now.Sub(w.lastListScan) < 700*time.Millisecond {
			continue
		}
		if best < 0 || late > bestLate {
			best, bestLate = i, late
		}
	}
	if best < 0 {
		return 0, false, false
	}
	w.mu.Lock()
	w.listDue[best] = false
	w.mu.Unlock()
	// now and then the whole area, for entries the window does not hold
	fullEvery := 10 * time.Second
	if now.Sub(firing[best]) < 1500*time.Millisecond {
		fullEvery = 4 * time.Second
	}
	return best, w.lists[best].win == nil || now.Sub(w.lists[best].lastFull) > fullEvery, true
}

// trackList keeps a window around a list's known entries, dropped after
// three reads without them.
func (w *Watcher) trackList(list int, rd ListRead, cw, ch int) {
	w.mu.Lock()
	mods := w.proc.fireLists.modules
	w.mu.Unlock()
	lw := &w.lists[list]
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	g := 0.0
	for _, e := range rd.Entries {
		if i, _ := MatchEntry(e, mods); i < 0 {
			continue
		}
		x0, y0 = math.Min(x0, e.X0), math.Min(y0, e.Y0)
		x1, y1 = math.Max(x1, e.X1), math.Max(y1, e.Y1)
		g = math.Max(g, e.G)
	}
	if g == 0 {
		if lw.win != nil {
			if lw.misses++; lw.misses >= 3 {
				lw.win = nil
			}
		}
		return
	}
	// room for the ammo lines, an entry above or below not seen this time
	// (entries are about 5 glyph heights apart), and the pilot's head moving
	win := vision.Rect{X0: int(x0 - 7*g), Y0: int(y0 - 8*g), X1: int(x1 + 7*g), Y1: int(y1 + 9*g)}.Clip(cw, ch)
	lw.win, lw.misses = &win, 0
}
