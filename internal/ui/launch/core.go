package launch

import (
	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/control"
)

// Core is what the window may ask of the core process. Every method
// returns at once: slow work goes to its own goroutine.
type Core interface {
	Status() control.Status
	Watch() (wake <-chan struct{}, stop func())
	Notices(after int64) []control.Notice
	WatchNotices() (wake <-chan struct{}, stop func())
	SetPaused(paused bool)
	PlayDemo()
	StopDemo()
	CalibrateGyro()
	OpenFile(which string) // control.FileSettings or control.FileLog
	OpenURL(address string)
	Quit()
}

// AppCore is the core as the tray runs it.
type AppCore struct {
	App              *app.App
	Addr             string // where EDSense sends to the controller app
	CfgPath, LogPath string
	Open             func(path string) // a text file, in Notepad
	Browse           func(address string)
	QuitApp          func()
}

var _ Core = (*AppCore)(nil)

func (c *AppCore) Status() control.Status {
	l, ok := c.App.Live()
	if !ok {
		l = app.Live{Status: c.App.Status(), Shield: -1, Heat: -1}
	}
	st := StatusOf(l, c.App.Words(), c.Addr)
	st.Full = ok
	return st
}

func (c *AppCore) Watch() (<-chan struct{}, func()) { return c.App.Watch() }

func (c *AppCore) Notices(after int64) []control.Notice {
	var out []control.Notice
	for _, n := range c.App.Notices(after) {
		out = append(out, NoticeOf(n))
	}
	return out
}

func (c *AppCore) WatchNotices() (<-chan struct{}, func()) { return c.App.WatchNotices() }
func (c *AppCore) SetPaused(p bool)                        { c.App.SetPaused(p) }
func (c *AppCore) PlayDemo()                               { c.App.RequestDemo() }
func (c *AppCore) StopDemo()                               { c.App.StopDemo() }
func (c *AppCore) CalibrateGyro()                          { c.App.CalibrateGyro() }
func (c *AppCore) OpenURL(address string)                  { c.Browse(address) }
func (c *AppCore) Quit()                                   { c.QuitApp() }

func (c *AppCore) OpenFile(which string) {
	switch which {
	case control.FileSettings:
		c.Open(c.CfgPath)
	case control.FileLog:
		c.Open(c.LogPath)
	}
}

// StatusOf is the window's view of l.
func StatusOf(l app.Live, w backend.Words, addr string) control.Status {
	level, text := app.Say(l.Status, w)
	st := control.Status{
		Level: map[app.Level]string{app.LevelActive: control.LevelActive, app.LevelIdle: control.LevelIdle, app.LevelError: control.LevelError}[level],
		Text:  text, Backend: l.Backend, Addr: addr, Online: l.Online,
		Elite: l.EliteRunning, Active: l.Active, Paused: l.Paused, Demo: l.Demo,
		DemoStep: l.DemoStep, DemoSteps: l.DemoSteps, Context: l.Context, FireGroup: l.FireGroup,
		Controllers: l.Controllers, Haptics: l.Haptics,
		Gyro: control.GyroStatus{Aim: l.GyroAim, By: l.GyroBy, Has: l.HasGyro, Aiming: l.GyroAiming,
			Calibrating: l.Calibrating, Calibrated: l.Calibrated, Drift: l.Drift},
		HUD: control.HUDStatus{On: l.HUDReader, Can: l.CanReadHUD, Shield: l.Shield, Heat: l.Heat},
	}
	if st.Backend == "" {
		st.Backend = w.Name
	}
	if st.Haptics == "" {
		st.Haptics = control.HapticsWaiting
	}
	return st
}

// NoticeOf is the window's view of n.
func NoticeOf(n app.Notice) control.Notice {
	level := control.NoticeInfo
	if n.Message {
		level = control.NoticeMessage
	}
	return control.Notice{ID: n.ID, Time: n.Time.UnixMilli(), Level: level, Text: n.Text}
}
