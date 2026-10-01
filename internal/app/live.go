package app

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
)

// Live is the status with what the window shows beside it. The loop
// publishes it only while someone watches, and never waits for anyone.
type Live struct {
	Status
	DemoStep, DemoSteps int // the demo's step, from 1

	Controllers int // listed by the backend
	Haptics     string

	GyroAim, HasGyro bool
	GyroBy           string
	GyroAiming       bool // EDSense's gyro aims now
	Calibrating      bool
	Calibrated       bool
	Drift            [3]float64

	FireGroup  int  // from 1; 0: not known
	HUDReader  bool // hud_reader is on
	CanReadHUD bool // the screen can be captured
	Shield     int  // %, -1: not read
	Heat       int  // %, -1: not read
}

// How the haptics play, in Live.Haptics.
const (
	HapticsNative  = "native"
	HapticsRumble  = "rumble"
	HapticsOff     = "off"
	HapticsWaiting = "waiting"
)

// wakeup wakes whoever waits on it, without ever waiting itself.
type wakeup struct {
	mu   sync.Mutex // for add and remove only
	subs atomic.Pointer[[]chan struct{}]
}

func (w *wakeup) add() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	var list []chan struct{}
	if p := w.subs.Load(); p != nil {
		list = append(list, *p...)
	}
	list = append(list, ch)
	w.subs.Store(&list)
	w.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			var rest []chan struct{}
			if p := w.subs.Load(); p != nil {
				for _, c := range *p {
					if c != ch {
						rest = append(rest, c)
					}
				}
			}
			w.subs.Store(&rest)
		})
	}
}

func (w *wakeup) wake() {
	p := w.subs.Load()
	if p == nil {
		return
	}
	for _, ch := range *p {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (w *wakeup) any() bool {
	p := w.subs.Load()
	return p != nil && len(*p) > 0
}

// liveStore holds the latest Live. Put and Get take no lock.
type liveStore struct {
	latest atomic.Pointer[Live]
	w      wakeup
}

// watched: someone waits for changes, so the loop fills Live in.
func (l *liveStore) watched() bool { return l.w.any() }

func (l *liveStore) put(v Live) {
	if p := l.latest.Load(); p != nil && *p == v {
		return
	}
	l.latest.Store(&v)
	l.w.wake()
}

func (l *liveStore) get() (Live, bool) {
	if p := l.latest.Load(); p != nil {
		return *p, true
	}
	return Live{}, false
}

// Watch wakes the returned channel whenever Live changes, until stop is
// called. The loop fills Live in only while someone watches.
func (a *App) Watch() (wake <-chan struct{}, stop func()) {
	ch, stop := a.live.w.add()
	a.wakeLoop() // the next tick fills it in
	return ch, stop
}

// Live is the latest status and detail; false until the loop has filled
// it in once.
func (a *App) Live() (Live, bool) { return a.live.get() }

// wakeLoop: a status for the new watcher even when nothing changes.
func (a *App) wakeLoop() { a.live.latest.Store(nil) }

// Notice is one line of the window's Activity.
type Notice struct {
	ID      int64
	Time    time.Time
	Message bool // a message box said it too
	Text    string
}

// NoticeCap is how many notices are kept.
const NoticeCap = 50

// notices keeps the latest NoticeCap notices.
type notices struct {
	mu   sync.Mutex
	ring [NoticeCap]Notice
	n    int // kept
	last int64
	w    wakeup
}

// add keeps a notice and returns its ID.
func (r *notices) add(message bool, text string) int64 {
	r.mu.Lock()
	r.last++
	id := r.last
	r.ring[id%NoticeCap] = Notice{ID: id, Time: time.Now(), Message: message, Text: text}
	r.n = min(r.n+1, NoticeCap)
	r.mu.Unlock()
	r.w.wake()
	return id
}

// since returns the notices after id, oldest first.
func (r *notices) since(id int64) []Notice {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Notice
	for i := r.last - int64(r.n) + 1; i <= r.last; i++ {
		if i > id {
			out = append(out, r.ring[i%NoticeCap])
		}
	}
	return out
}

// Notices are the latest notices after id (0: all kept), oldest first.
func (a *App) Notices(after int64) []Notice { return a.notices.since(after) }

// WatchNotices wakes the returned channel on each new notice, until stop
// is called.
func (a *App) WatchNotices() (wake <-chan struct{}, stop func()) { return a.notices.w.add() }

// note keeps a line for the window's Activity.
func (a *App) note(text string) { a.notices.add(false, text) }

// detail is what Live adds to the status. It reads only what the loop
// already holds, and asks nothing of the backend, the pad or the system:
// with the window open, every tick does exactly what it does without.
// own: EDSense's gyro can aim, as this tick found.
func (s *session) detail(now time.Time, st Status, own bool) Live {
	l := Live{Status: st, Controllers: len(s.controllers),
		GyroAim: s.cfg.GyroAim, GyroBy: s.cfg.GyroBy, HasGyro: s.gyro != nil,
		HUDReader: s.cfg.HUDReader, CanReadHUD: s.hud != nil, Shield: -1, Heat: -1}
	native := s.caps.Haptics && s.cfg.HapticsMode != config.HapticsRumble
	switch {
	case !s.cfg.Haptics:
		l.Haptics = HapticsOff
	case s.nativeOn:
		l.Haptics = HapticsNative
	case !native && s.caps.Rumble:
		l.Haptics = HapticsRumble
	default:
		l.Haptics = HapticsWaiting
	}
	if s.gyro != nil {
		g := s.gyroStatus()
		l.Calibrating, l.Calibrated, l.Drift = g.Calibrating, g.Calibrated, g.Bias
		l.GyroAiming = own && s.hold == 0
	}
	if s.running && s.game.HaveStatus && s.game.Status.InShip() {
		l.FireGroup = s.game.Status.FireGroup + 1
	}
	if hs := s.game.HUD; s.hud != nil {
		if hs.Shield.Fresh(now, hudFresh) {
			l.Shield = hs.Shield.Value
		}
		if hs.Heat.Fresh(now, hudFresh) {
			l.Heat = hs.Heat.Value
		}
	}
	return l
}

// hudFresh: an older HUD reading is not shown.
const hudFresh = 10 * time.Second

// Level is the colour of a status: the tray icon's.
type Level int

const (
	LevelIdle Level = iota
	LevelActive
	LevelError
)

// Say is the status in the tray's words, and its colour.
func Say(s Status, w backend.Words) (Level, string) {
	switch {
	case s.Demo:
		return LevelActive, "Playing the demo"
	case !s.Online:
		return LevelError, w.TrayOffline
	case s.Paused:
		return LevelIdle, "Paused"
	case !s.EliteRunning:
		return LevelIdle, "Waiting for Elite Dangerous"
	case s.Active:
		return LevelActive, "Active: " + s.Context
	}
	return LevelIdle, "Elite running, not in a ship or on foot yet"
}
