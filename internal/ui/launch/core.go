package launch

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/engine"
	"github.com/tolgahan/ed-sense/internal/install"
)

// Core is what the window may ask of the core process. The methods after
// "these may wait" can take a while (a switch, a probe, a file write), so
// the window asks them off its reader; the others return at once.
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
	Settings() control.Settings
	WatchSettings() (wake <-chan struct{}, stop func())
	Schema() any

	// these may wait
	PatchSettings(patch []byte) (control.Patched, error)
	ApplyNow() (control.Engine, error)
	// Choose: a *engine.NotSavedError when it works but is not saved;
	// keepPin only saves while -backend decides this run ("Set up later")
	Choose(choice string, keepPin bool) (control.Engine, error)
	Detect(fresh bool) control.Detection
	CheckSetup(app string, fresh bool) control.Setup
	ResetProfile(app string) control.ProfileReset
}

// Engine is the controller app engine, as the window uses it.
type Engine interface {
	State() engine.State
	ApplyWait(why string, wait time.Duration) (engine.State, error)
	ChooseWait(choice, from string, wait time.Duration) (engine.State, error)
	SaveWait(choice, from string, wait time.Duration) (engine.State, error)
	Detect(fresh bool) engine.Detection
}

// Store is the settings file, as the window uses it.
type Store interface {
	Snapshot() *config.Snapshot
	Watch() (wake <-chan struct{}, stop func())
	Patch(patch []byte, from string) (config.PatchResult, error)
}

var (
	_ Engine = (*engine.Engine)(nil)
	_ Store  = (*config.Store)(nil)
)

// OpWait is how long the window's Apply now and choice wait for a switch
// under way before they say busy.
const OpWait = 5 * time.Second

// AppCore is the core as the tray runs it.
type AppCore struct {
	App              *app.App
	Engine           Engine
	Store            Store
	CfgPath, LogPath string
	Open             func(path string) // a text file, in Notepad
	Browse           func(address string)
	QuitApp          func()

	checkOnce sync.Once
	checker   *install.Checker
}

var _ Core = (*AppCore)(nil)

func (c *AppCore) Status() control.Status {
	l, ok := c.App.Live()
	if !ok {
		l = app.Live{Status: c.App.Status(), Shield: -1, Heat: -1}
	}
	w := c.App.Words()
	if l.Kind != "" { // the words of the status' own backend, during a switch too
		w = backend.WordsFor(l.Kind)
	}
	st := StatusOf(l, w, "")
	st.Full = ok
	if c.Engine != nil {
		WithEngine(&st, c.Engine.State())
	}
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

func (c *AppCore) Settings() control.Settings               { return SettingsOf(c.Store.Snapshot()) }
func (c *AppCore) WatchSettings() (<-chan struct{}, func()) { return c.Store.Watch() }
func (c *AppCore) Schema() any                              { return config.Schema() }
func (c *AppCore) Detect(fresh bool) control.Detection      { return DetectionOf(c.Engine.Detect(fresh)) }
func (c *AppCore) CheckSetup(app string, fresh bool) control.Setup {
	return c.checks().Check(app, fresh)
}

func (c *AppCore) PatchSettings(patch []byte) (control.Patched, error) {
	r, err := c.Store.Patch(patch, "window")
	if err != nil {
		return control.Patched{Rev: r.Rev, Problems: []control.Problem{}}, err
	}
	return PatchedOf(r), nil
}

func (c *AppCore) ApplyNow() (control.Engine, error) {
	st, err := c.Engine.ApplyWait("Apply now", OpWait)
	return EngineOf(st), err
}

func (c *AppCore) Choose(choice string, keepPin bool) (control.Engine, error) {
	choose := c.Engine.ChooseWait
	if keepPin {
		choose = c.Engine.SaveWait
	}
	st, err := choose(choice, "window", OpWait)
	return EngineOf(st), err
}

// The DSX profile reset's texts.
const (
	ResetWaits = "EDSense puts its DSX profile back as soon as DSX is closed (DSX tray icon > Exit). Then start DSX again."
	ResetLater = "The DSX profile can be reset while EDSense uses DSX."
)

// ResetProfile has the session put EDSense's DSX profile back, as the
// tray's reset does: once DSX is closed.
func (c *AppCore) ResetProfile(name string) control.ProfileReset {
	if c.App.Backend() != backend.KindDSX {
		return control.ProfileReset{App: name, State: control.ResetUnavailable, Text: ResetLater}
	}
	log.Print("DSX profile reset requested")
	c.App.RequestDSXProfileReset()
	return control.ProfileReset{App: name, State: control.ResetWaiting, Text: ResetWaits}
}

// checks is the setup checklists' gatherer.
func (c *AppCore) checks() *install.Checker {
	c.checkOnce.Do(func() {
		c.checker = &install.Checker{
			Status:  c.Status,
			Detect:  c.Detect,
			Report:  c.App.SetupReport,
			Recheck: c.App.CheckSetup,
		}
	})
	return c.checker
}

// StatusOf is the window's view of l.
func StatusOf(l app.Live, w backend.Words, addr string) control.Status {
	level, text := app.Say(l.Status, w)
	st := control.Status{
		Level: map[app.Level]string{app.LevelActive: control.LevelActive, app.LevelIdle: control.LevelIdle, app.LevelError: control.LevelError}[level],
		Text:  text, Backend: l.Backend, Addr: addr, Online: l.Online, Kind: string(l.Kind),
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

// WithEngine fills in what the engine runs: the app, the choice and why,
// a switch under way and the settings that wait for Apply now.
func WithEngine(st *control.Status, e engine.State) {
	st.Addr, st.Kind, st.Choice, st.Pinned, st.Why = e.Addr, string(e.Kind), e.Choice, e.Pinned, e.Why
	st.Switching, st.Pending = e.Switching, orEmpty(e.Pending)
}

// EngineOf is the window's view of e.
func EngineOf(e engine.State) control.Engine {
	return control.Engine{Choice: e.Choice, Pinned: e.Pinned, Kind: string(e.Kind), Name: e.Name, Why: e.Why,
		Addr: e.Addr, Switching: e.Switching, Pending: orEmpty(e.Pending)}
}

// DetectionOf is the window's view of d.
func DetectionOf(d engine.Detection) control.Detection {
	return control.Detection{T: d.T,
		DSX:        control.Seen{Running: d.DSX.Running, Addr: d.DSX.Addr, Answers: d.DSX.Answers},
		DS4Windows: control.Seen{Running: d.DS4W.Running, Version: d.DS4W.Version, Addr: d.DS4W.Addr, Answers: d.DS4W.Answers, Dir: d.DS4W.Dir},
		Auto:       control.AutoPick{Kind: string(d.Auto.Kind), Why: d.Auto.Why, Sure: d.Auto.Sure},
	}
}

// SettingsOf is the window's view of s.
func SettingsOf(s *config.Snapshot) control.Settings {
	out := control.Settings{Rev: s.Rev, Config: s.Config, FirstRun: s.FirstRun}
	if p := s.Problem; p != nil {
		out.Broken = &control.Broken{Line: p.Line, Col: p.Col, Key: p.Key, Msg: p.Msg}
	}
	return out
}

// PatchedOf is the window's view of r.
func PatchedOf(r config.PatchResult) control.Patched {
	out := control.Patched{Applied: r.Applied, Rev: r.Rev, Problems: []control.Problem{}}
	for _, p := range r.Problems {
		out.Problems = append(out.Problems, control.Problem{Path: p.Path, Code: p.Code, Msg: p.Msg})
	}
	return out
}

// ErrorOf is how an engine or settings error reaches the window: busy and
// broken by their codes, anything else as failed. Full paths are cut to
// the file's name.
func ErrorOf(err error) *control.Error {
	switch {
	case errors.Is(err, engine.ErrBusy):
		return &control.Error{Code: control.CodeBusy, Msg: err.Error()}
	case errors.Is(err, engine.ErrBroken):
		return &control.Error{Code: control.CodeBroken, Msg: Plain(err)}
	}
	return &control.Error{Code: control.CodeFailed, Msg: Plain(err)}
}

// Plain is err's text with the files it names cut to their names: the
// window never shows a full path.
func Plain(err error) string {
	msg := err.Error()
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Path != "" {
		msg = strings.ReplaceAll(msg, pe.Path, filepath.Base(pe.Path))
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		for _, p := range []string{le.Old, le.New} {
			if p != "" {
				msg = strings.ReplaceAll(msg, p, filepath.Base(p))
			}
		}
	}
	return msg
}
