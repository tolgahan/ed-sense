// Package tray runs EDSense from the notification area.
package tray

import (
	"errors"
	"log"
	"path/filepath"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"github.com/tolgahan/ed-sense/assets"
	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/engine"
	"github.com/tolgahan/ed-sense/internal/platform"
	"github.com/tolgahan/ed-sense/internal/ui/launch"
)

const name = "EDSense"

// Options are the tray's files and words.
type Options struct {
	CfgPath, LogPath, Version string
	TrayOnly                  bool // started with -tray: no window at start
}

// Run shows the tray icon and runs a, through eng, until the player quits.
// Settings change through store, and the controller app through eng.
func Run(a *app.App, eng *engine.Engine, store *config.Store, o Options) {
	cfgPath, logPath, version := o.CfgPath, o.LogPath, o.Version
	mutex, ok := takeInstance(o.TrayOnly)
	if !ok {
		return
	}
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}

	m := &menu{app: a, eng: eng, store: store, cfgPath: cfgPath, logPath: logPath, version: version}
	m.win = newWindow(a, eng, o)
	// before the loop starts: its first messages come at once
	a.SetNotify(func(id int64, msg string) { go m.say(id, msg) })
	stopListening, err := launch.ListenForOpen(m.win.Open)
	if err != nil {
		log.Printf("Window: a second start cannot open it: %v", err)
	}
	if !o.TrayOnly {
		m.win.Open()
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		eng.Run(stop)
		close(stopped)
	}()
	systray.Run(m.build, func() {
		stopListening() // a start from now on is the next EDSense
		close(stop)
		closed := make(chan struct{})
		go func() {
			m.win.Close(1500 * time.Millisecond)
			close(closed)
		}()
		timeout := time.After(2 * time.Second)
		for stopped != nil || closed != nil {
			select {
			case <-stopped:
				stopped = nil
			case <-closed:
				closed = nil
			case <-timeout:
				return
			}
		}
	})
}

// newWindow is the EDSense window, started on demand as its own process.
func newWindow(a *app.App, eng *engine.Engine, o Options) *launch.Window {
	said := false
	return launch.New(launch.Config{
		Version:   o.Version,
		StatePath: filepath.Join(filepath.Dir(o.CfgPath), "ui_state.json"),
		Core: &launch.AppCore{App: a, Addr: func() string { return eng.State().Addr }, CfgPath: o.CfgPath, LogPath: o.LogPath,
			Open: platform.OpenInEditor, Browse: platform.OpenURL, QuitApp: systray.Quit},
		Ask:  func(text string) bool { return platform.AskYesNo(name, text) },
		Warn: func(text string) { platform.ShowError(name, text) },
		Missing: func() bool {
			missing, found := launch.RuntimeMissing()
			if !missing && !said {
				log.Printf("Window: WebView2 Runtime %s", found)
				said = true
			}
			return missing
		},
	})
}

type menu struct {
	app                       *app.App
	eng                       *engine.Engine
	store                     *config.Store
	cfgPath, logPath, version string
	status                    *systray.MenuItem
	pause                     *systray.MenuItem
	gyro, ownGyro             *systray.MenuItem
	apps                      map[string]*systray.MenuItem // the Controller app items, by backend setting
	profile                   *systray.MenuItem
	win                       *launch.Window
}

func (m *menu) build() {
	st := m.eng.State()
	words := backend.WordsFor(st.Kind)
	systray.SetIcon(assets.IconIdle)
	systray.SetTitle(name)
	systray.SetTooltip(name + " " + m.version)
	// a left click opens the window; the menu stays on the right click
	systray.SetOnTapped(func() { go m.win.Open() })

	m.status = systray.AddMenuItem("Starting...", "")
	m.status.Disable()
	systray.AddSeparator()
	open := systray.AddMenuItem("Open EDSense", "Open the EDSense window")
	pause := systray.AddMenuItemCheckbox("Pause effects", words.PauseTip, false)
	m.pause = pause
	demo := systray.AddMenuItem("Play demo", "Play every effect once")
	systray.AddSeparator()
	cfg := m.store.Snapshot().Config
	gyro := systray.AddMenuItemCheckbox("Gyro aim", "Off: no gyro aim while Elite runs, and the turn feel follows the sticks", cfg.GyroAim)
	ownGyro := systray.AddMenuItemCheckbox("EDSense gyro", words.OwnGyroTip, cfg.GyroBy == config.GyroByEDSense)
	m.gyro, m.ownGyro = gyro, ownGyro
	calibrate := systray.AddMenuItem("Calibrate gyro...", "Learn the controller's drift: put it down for 2 seconds")
	settings := systray.AddMenuItem("Open settings", m.cfgPath)
	logFile := systray.AddMenuItem("Open log", "")
	controllerApp := systray.AddMenuItem("Controller app", "What EDSense drives the controller through")
	m.apps = map[string]*systray.MenuItem{}
	for _, c := range []struct{ setting, title, tip string }{
		{config.BackendAuto, "Auto", "The one that runs; with both, the one that answers; with neither, DSX"},
		{config.BackendDSX, "DSX", "DSX, with its Incoming UDP"},
		{config.BackendDS4Windows, "DS4Windows", "DS4Windows 5, with its game mod support"},
	} {
		m.apps[c.setting] = controllerApp.AddSubMenuItemCheckbox(c.title, c.tip, cfg.BackendChoice() == c.setting)
	}
	profile := systray.AddMenuItem("Reset DSX profile...", "Replace DSX's \"Elite Dangerous\" controller profile with the one that comes with EDSense")
	m.profile = profile
	if st.Kind != backend.KindDSX {
		profile.Hide()
	}
	systray.AddSeparator()
	systray.AddMenuItem(name+" "+m.version, "").Disable()
	quit := systray.AddMenuItem("Quit", "")

	m.app.OnStatus(m.show)
	go m.followSettings()
	go m.followEngine(st.Kind)
	go func() {
		for {
			select {
			case <-open.ClickedCh:
				m.win.Open()
			case <-pause.ClickedCh:
				m.app.SetPaused(toggle(pause))
			case <-demo.ClickedCh:
				m.app.RequestDemo()
			case <-gyro.ClickedCh:
				m.setGyroAim()
			case <-ownGyro.ClickedCh:
				m.setGyroBy()
			case <-calibrate.ClickedCh:
				go m.calibrateGyro()
			case <-settings.ClickedCh:
				platform.OpenInEditor(m.cfgPath)
			case <-logFile.ClickedCh:
				platform.OpenInEditor(m.logPath)
			case <-profile.ClickedCh:
				go m.resetProfile()
			case <-m.apps[config.BackendAuto].ClickedCh:
				go m.setBackend(config.BackendAuto)
			case <-m.apps[config.BackendDSX].ClickedCh:
				go m.setBackend(config.BackendDSX)
			case <-m.apps[config.BackendDS4Windows].ClickedCh:
				go m.setBackend(config.BackendDS4Windows)
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// startWait is how long a message waits for a window that is starting.
const startWait = 30 * time.Second

// say shows one of EDSense's messages (notice id): in the window when it
// shows it, else in a box.
func (m *menu) say(id int64, msg string) {
	if m.win.Takes(id, startWait) {
		return
	}
	platform.ShowInfo(name, msg)
}

// takeInstance makes this the running EDSense. When another one runs, it
// asks that one to open its window and reports false; with trayOnly it
// asks nothing. An EDSense that has just started may not listen yet, and
// one that quits lets go soon, so this tries for a moment.
func takeInstance(trayOnly bool) (windows.Handle, bool) {
	mutexName, _ := windows.UTF16PtrFromString(platform.InstanceMutex)
	for end := time.Now().Add(3 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		mutex, err := windows.CreateMutex(nil, false, mutexName)
		// ACCESS_DENIED: another EDSense holds it, running as administrator
		if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return mutex, true
		}
		if mutex != 0 {
			windows.CloseHandle(mutex) // held open, it would keep the other one's name alive
		}
		if trayOnly || launch.SignalRunning() {
			return 0, false
		}
		if time.Now().After(end) {
			platform.ShowInfo(name, "EDSense is already running.\n\nLook for its icon next to the clock (you may need to click the ^ arrow).")
			return 0, false
		}
	}
}

// toggle flips a checkbox and returns its new state.
func toggle(item *systray.MenuItem) bool {
	if item.Checked() {
		item.Uncheck()
		return false
	}
	item.Check()
	return true
}

func (m *menu) show(s app.Status) {
	w := m.app.Words()
	if s.Kind != "" { // the status' own backend, during a switch too
		w = backend.WordsFor(s.Kind)
	}
	icon, text := look(s, w)
	systray.SetIcon(icon)
	tip := name + " - " + text
	if len(tip) > 120 {
		tip = tip[:120]
	}
	systray.SetTooltip(tip)
	m.status.SetTitle(text)
	// the window can pause too
	if s.Paused != m.pause.Checked() && !s.Demo {
		if s.Paused {
			m.pause.Check()
		} else {
			m.pause.Uncheck()
		}
	}
}

func look(s app.Status, w backend.Words) (icon []byte, text string) {
	level, text := app.Say(s, w)
	switch level {
	case app.LevelActive:
		return assets.IconActive, text
	case app.LevelError:
		return assets.IconError, text
	}
	return assets.IconIdle, text
}

// followSettings keeps the gyro and Controller app checkmarks on what the
// settings file holds, whoever changed it: the tray, the window or an
// editor. Only it sets them.
func (m *menu) followSettings() {
	wake, _ := m.store.Watch() // for as long as the tray runs
	for {
		cfg := m.store.Snapshot().Config
		check(m.gyro, cfg.GyroAim)
		check(m.ownGyro, cfg.GyroBy == config.GyroByEDSense)
		for setting, item := range m.apps {
			check(item, cfg.BackendChoice() == setting)
		}
		<-wake
	}
}

// followEngine keeps the tips and the DSX reset item on the app EDSense
// runs on. Only it sets them. kind is the app the menu was built for.
func (m *menu) followEngine(kind backend.Kind) {
	wake, _ := m.eng.Watch() // for as long as the tray runs
	for {
		st := m.eng.State()
		if st.Kind != kind {
			kind = st.Kind
			w := backend.WordsFor(kind)
			m.pause.SetTooltip(w.PauseTip)
			m.ownGyro.SetTooltip(w.OwnGyroTip)
			if kind == backend.KindDSX {
				m.profile.Show()
			} else {
				m.profile.Hide()
			}
		}
		<-wake
	}
}

func check(item *systray.MenuItem, on bool) {
	if on {
		item.Check()
	} else {
		item.Uncheck()
	}
}

// setGyroAim turns gyro_aim over from what the settings hold; the
// checkmark follows the file.
func (m *menu) setGyroAim() {
	on := !m.store.Snapshot().Config.GyroAim
	if err := m.store.Set(func(c *config.Config) { c.GyroAim = on }, "tray"); err != nil {
		log.Printf("Gyro aim: %v", err)
		platform.ShowError(name, "Could not change \"Gyro aim\":\n"+err.Error())
		return
	}
	log.Printf("Gyro aim %s", onOff(on))
}

func (m *menu) setGyroBy() {
	by := config.GyroByEDSense
	if m.store.Snapshot().Config.GyroBy == config.GyroByEDSense {
		by = config.GyroByDSX
	}
	if err := m.store.Set(func(c *config.Config) { c.GyroBy = by }, "tray"); err != nil {
		log.Printf("EDSense gyro: %v", err)
		platform.ShowError(name, "Could not change \"EDSense gyro\":\n"+err.Error())
		return
	}
	log.Printf("EDSense gyro %s", onOff(by == config.GyroByEDSense))
}

func (m *menu) calibrateGyro() {
	if platform.AskYesNo(name, "Put the controller down on a flat surface and let go, then press Yes.\n\nKeep it still for 2 seconds.") {
		m.app.CalibrateGyro()
	}
}

func (m *menu) resetProfile() {
	if platform.AskYesNo(name, "Replace DSX's \""+dsx.ProfileName+"\" controller profile with the one that comes with EDSense?\n\n"+
		"Your current profile is kept in the dsx_profile_backups folder next to EDSense.\n\n"+
		"DSX must be closed for this. If it is running, close it now (DSX tray icon > Exit): EDSense does the reset as soon as DSX is closed. Then start DSX again.") {
		log.Print("DSX profile reset requested")
		m.app.RequestDSXProfileReset()
	}
}

// setBackend switches the controller app at once, and saves it; the
// checkmarks follow the file.
func (m *menu) setBackend(setting string) {
	_, err := m.eng.Choose(setting, "tray")
	if err == nil {
		return
	}
	log.Printf("Controller app: %v", err)
	var notSaved *engine.NotSavedError
	if errors.As(err, &notSaved) {
		platform.ShowError(name, "The controller app changed until EDSense quits, but it could not be saved:\n"+notSaved.Err.Error())
		return
	}
	platform.ShowError(name, "Could not change the controller app:\n"+err.Error())
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
