package control

import (
	"time"
)

// Status is what the window shows of the core, sent as the "status"
// event and as status.get's answer.
type Status struct {
	Full  bool   `json:"full"`  // the detail below Text is filled in; else only the tray's part is
	Level string `json:"level"` // the tray icon's colour: LevelActive, LevelIdle or LevelError
	Text  string `json:"text"`  // the tray's sentence

	Backend string `json:"backend"` // the controller app's name
	Addr    string `json:"addr"`    // where EDSense sends to it
	Online  bool   `json:"online"`  // it answers

	Elite     bool   `json:"elite"` // Elite runs
	Active    bool   `json:"active"`
	Paused    bool   `json:"paused"`
	Demo      bool   `json:"demo"`
	DemoStep  int    `json:"demo_step,omitempty"` // from 1
	DemoSteps int    `json:"demo_steps,omitempty"`
	Context   string `json:"context"`
	FireGroup int    `json:"fire_group"` // from 1; 0: not known

	Controllers int    `json:"controllers"` // listed by the controller app
	Pad         bool   `json:"pad"`         // the virtual DualSense is open
	Haptics     string `json:"haptics"`     // HapticsNative, HapticsRumble, HapticsOff or HapticsWaiting

	Gyro GyroStatus `json:"gyro"`
	HUD  HUDStatus  `json:"hud"`
}

// Status levels.
const (
	LevelActive = "active"
	LevelIdle   = "idle"
	LevelError  = "error"
)

// How the haptics play.
const (
	HapticsNative  = "native"
	HapticsRumble  = "rumble"
	HapticsOff     = "off"
	HapticsWaiting = "waiting" // on, but nothing plays them yet
)

// GyroStatus is the gyro aim.
type GyroStatus struct {
	Aim         bool       `json:"aim"`    // gyro_aim is on
	By          string     `json:"by"`     // gyro_by: who should aim
	Has         bool       `json:"has"`    // the connection passes motion
	Aiming      bool       `json:"aiming"` // EDSense's gyro aims now
	Calibrating bool       `json:"calibrating"`
	Calibrated  bool       `json:"calibrated"`
	Drift       [3]float64 `json:"drift"` // deg/s
}

// HUDStatus is the HUD reader.
type HUDStatus struct {
	On     bool `json:"on"`     // hud_reader is on
	Can    bool `json:"can"`    // the screen can be captured here
	Shield int  `json:"shield"` // %, -1: not read
	Heat   int  `json:"heat"`   // %, -1: not read
}

// Notice is one line of the window's Activity, sent as the "notice" event
// and in notices.list's answer.
type Notice struct {
	ID    int64  `json:"id"`
	Time  int64  `json:"t"` // Unix milliseconds
	Level string `json:"level"`
	Text  string `json:"text"`
}

// Notice levels.
const (
	NoticeInfo    = "info"    // something happened
	NoticeMessage = "message" // a message box said it too
)

// Pacer spaces out sends: at most one per Every. Nothing is lost, only
// merged: the next send carries the latest value.
type Pacer struct {
	Every time.Duration
	last  time.Time
}

// Wait is how long to wait before the next send at now.
func (p *Pacer) Wait(now time.Time) time.Duration {
	if p.last.IsZero() {
		return 0
	}
	if d := p.Every - now.Sub(p.last); d > 0 {
		return d
	}
	return 0
}

// Sent records a send at now.
func (p *Pacer) Sent(now time.Time) { p.last = now }

// StatusEvery is the shortest time between two status events.
const StatusEvery = 250 * time.Millisecond

// Pump sends the latest status whenever wake fires, at most once per
// every, until stop is closed. send returns false when the value could
// not be queued; it is tried again after every.
func Pump(stop <-chan struct{}, wake <-chan struct{}, every time.Duration, send func() bool) {
	p := Pacer{Every: every}
	dirty := true // the first value goes at once
	for {
		if !dirty {
			select {
			case <-stop:
				return
			case <-wake:
			}
		}
		if d := p.Wait(time.Now()); d > 0 {
			t := time.NewTimer(d)
			select {
			case <-stop:
				t.Stop()
				return
			case <-t.C:
			}
		}
		// what woke us meanwhile is in the value sent now
		select {
		case <-wake:
		default:
		}
		dirty = !send()
		p.Sent(time.Now())
		if dirty {
			t := time.NewTimer(every)
			select {
			case <-stop:
				t.Stop()
				return
			case <-t.C:
			}
		}
	}
}
