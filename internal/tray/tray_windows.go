// Package tray runs EDSense from the notification area.
package tray

import (
	"errors"
	"log"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"github.com/tolgahan/ed-sense/assets"
	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/platform"
)

const name = "EDSense"

// Run shows the tray icon and runs a until the player quits.
func Run(a *app.App, cfgPath, logPath, version string) {
	mutexName, _ := windows.UTF16PtrFromString(`Local\EDSense-single-instance`)
	mutex, err := windows.CreateMutex(nil, false, mutexName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		platform.ShowInfo(name, "EDSense is already running.\n\nLook for its icon next to the clock (you may need to click the ^ arrow).")
		return
	}
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}

	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		a.Run(stop)
		close(stopped)
	}()
	m := &menu{app: a, cfgPath: cfgPath, logPath: logPath, version: version}
	systray.Run(m.build, func() {
		close(stop)
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
	})
}

type menu struct {
	app                       *app.App
	cfgPath, logPath, version string
	status                    *systray.MenuItem
}

func (m *menu) build() {
	m.app.SetNotify(func(msg string) { go platform.ShowInfo(name, msg) })
	systray.SetIcon(assets.IconIdle)
	systray.SetTitle(name)
	systray.SetTooltip(name + " " + m.version)

	m.status = systray.AddMenuItem("Starting...", "")
	m.status.Disable()
	systray.AddSeparator()
	pause := systray.AddMenuItemCheckbox("Pause effects", "Hand the controller back to your DSX profile", false)
	demo := systray.AddMenuItem("Play demo", "Play every effect once")
	systray.AddSeparator()
	cfg, _ := config.Load(m.cfgPath)
	gyro := systray.AddMenuItemCheckbox("Gyro aim", "Off: motion aiming is off while Elite runs, and the turn feel follows the sticks", cfg.GyroAim)
	settings := systray.AddMenuItem("Open settings", m.cfgPath)
	logFile := systray.AddMenuItem("Open log", "")
	profile := systray.AddMenuItem("Reset DSX profile...", "Replace DSX's \"Elite Dangerous\" controller profile with the one that comes with EDSense")
	systray.AddSeparator()
	systray.AddMenuItem(name+" "+m.version, "").Disable()
	quit := systray.AddMenuItem("Quit", "")

	m.app.OnStatus(m.show)
	go func() {
		for {
			select {
			case <-pause.ClickedCh:
				m.app.SetPaused(toggle(pause))
			case <-demo.ClickedCh:
				m.app.RequestDemo()
			case <-gyro.ClickedCh:
				m.setGyroAim(gyro)
			case <-settings.ClickedCh:
				platform.OpenInEditor(m.cfgPath)
			case <-logFile.ClickedCh:
				platform.OpenInEditor(m.logPath)
			case <-profile.ClickedCh:
				go m.resetProfile()
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
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
	icon, text := look(s)
	systray.SetIcon(icon)
	tip := name + " - " + text
	if len(tip) > 120 {
		tip = tip[:120]
	}
	systray.SetTooltip(tip)
	m.status.SetTitle(text)
}

func look(s app.Status) (icon []byte, text string) {
	switch {
	case s.Demo:
		return assets.IconActive, "Playing the demo"
	case !s.DSXOnline:
		return assets.IconError, "DSX not connected (DSX > Settings > Networking > Incoming UDP)"
	case s.Paused:
		return assets.IconIdle, "Paused"
	case !s.EliteRunning:
		return assets.IconIdle, "Waiting for Elite Dangerous"
	case s.Active:
		return assets.IconActive, "Active: " + s.Context
	}
	return assets.IconIdle, "Elite running, not in a ship or on foot yet"
}

func (m *menu) setGyroAim(item *systray.MenuItem) {
	on := !item.Checked()
	if err := config.Update(m.cfgPath, func(c *config.Config) { c.GyroAim = on }); err != nil {
		log.Printf("Gyro aim: %v", err)
		platform.ShowError(name, "Could not change \"Gyro aim\":\n"+err.Error())
		return
	}
	toggle(item)
	log.Printf("Gyro aim %s", onOff(on))
}

func (m *menu) resetProfile() {
	if platform.AskYesNo(name, "Replace DSX's \""+dsx.ProfileName+"\" controller profile with the one that comes with EDSense?\n\n"+
		"Your current profile is kept in the dsx_profile_backups folder next to EDSense.\n\n"+
		"DSX must be closed for this. If it is running, close it now (DSX tray icon > Exit): EDSense does the reset as soon as DSX is closed. Then start DSX again.") {
		log.Print("DSX profile reset requested")
		m.app.RequestDSXProfileReset()
	}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
