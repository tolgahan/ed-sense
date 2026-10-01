//go:build windows

package window

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/tolgahan/ed-sense/assets"
	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ui/web"
)

// Page colours (--bg in app.css), set before the first paint so the
// window never flashes white.
var (
	bgLight = application.NewRGB(0xF7, 0xF6, 0xF3)
	bgDark  = application.NewRGB(0x1C, 0x1B, 0x1A)
)

// callAlias is the id the page passes to Call.ByID for Bridge.Call, so the
// Go package path never has to appear in the web files.
const callAlias = 1

// bridgeCallID is the id Wails gives Bridge.Call: FNV-1a of
// "<package path>.Bridge.Call" (pkg/application/bindings.go).
func bridgeCallID() uint32 {
	t := reflect.TypeOf(Bridge{})
	h := fnv.New32a()
	h.Write([]byte(t.PkgPath() + "." + t.Name() + ".Call"))
	return h.Sum32()
}

// helloTimeout: the core answers at once when it started us.
const helloTimeout = 5 * time.Second

// Run shows the window and returns the exit code once it has closed.
func Run(opt Options) int {
	// Nothing started from here (WebView2's processes) may inherit the
	// pipes to the core.
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		_ = windows.SetHandleInformation(windows.Handle(f.Fd()), windows.HANDLE_FLAG_INHERIT, 0)
	}
	pipe := os.Stdout
	os.Stdout = os.Stderr
	log.SetOutput(os.Stderr) // Wails' WebView2 layer logs here too; the core adds the time
	log.SetFlags(0)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := systemDLLsOnly(); err != nil {
		logger.Warn("DLL search path", "err", err)
	}

	if !production {
		logger.Error("this build lacks -tags production, so the window stays closed")
		return exitDevOnly
	}
	if !fromCore(os.Stdin) {
		// started by something else, such as a pinned taskbar button:
		// the tray EDSense opens the window instead
		startCore()
		return exitNoCore
	}

	// the pipe to the core
	conn := control.NewConn(pipe, nil)
	client := control.NewClient(conn)
	var app atomic.Pointer[application.App]
	var win atomic.Pointer[application.WebviewWindow]
	var bp atomic.Pointer[Bridge]
	gone := make(chan struct{}) // stdin ended, or the core said bye
	var goneOnce sync.Once
	leave := func() { goneOnce.Do(func() { close(gone) }) }
	go func() {
		err := control.Read(os.Stdin, func(l control.Line) {
			if client.Deliver(l) {
				return
			}
			switch {
			case l.Ev == "bye":
				leave()
			case l.Ev == "focus":
				if w := win.Load(); w != nil {
					w.Focus()
				}
			case toPage(l.Ev):
				if b := bp.Load(); b != nil && l.Ev == "notice" {
					var n struct {
						ID int64 `json:"id"`
					}
					if json.Unmarshal(l.D, &n) == nil {
						b.sawNotice(n.ID)
					}
				}
				if a := app.Load(); a != nil {
					a.Event.Emit("edsense:"+l.Ev, l.D)
				}
			}
		})
		if err != nil {
			logger.Warn("pipe from the core", "err", err)
		}
		client.Close()
		leave()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), helloTimeout)
	raw, err := client.Call(ctx, "hello", control.Hello{Proto: control.Proto})
	cancel()
	protoOK := true
	var welcome control.Welcome
	var cerr *control.Error
	switch {
	case errors.As(err, &cerr) && cerr.Code == control.CodeProto:
		protoOK = false // the page says to restart EDSense
	case err != nil:
		logger.Error("no answer from the core", "err", err)
		return exitNoCore
	default:
		_ = json.Unmarshal(raw, &welcome)
	}

	dataDir, err := userDataPath()
	if err != nil {
		logger.Error("user data folder", "err", err)
		return exitFailed
	}

	// Windows' Text size as the page last got it
	var scale atomic.Int32
	pageScale := func() int {
		n := textScale()
		scale.Store(int32(n))
		return n
	}
	bridge := &Bridge{Core: client, Version: opt.Version, ProtoOK: protoOK, After: welcome.After, About: about, Front: allowFrontAny, TextScale: pageScale}
	bp.Store(bridge)
	bridge.setPage(welcome.Page)
	place := welcome.Place
	bridge.setTheme(place.Theme)

	var showOnce sync.Once
	var track func()
	var shown atomic.Bool // until then the saved placement stands
	show := func() {
		showOnce.Do(func() {
			a, w := app.Load(), win.Load()
			placeWindow(a, w, place)
			shown.Store(true)
			track()
			if place.Maximized {
				w.Maximise() // Wails' Maximise leaves the page hidden: Show shows it
			}
			w.Show()
			w.Focus()
		})
	}
	// Show once the page has built its first view and WebView2 has finished
	// the first navigation, so Wails' hide and show of the controller at
	// that point happens while the window is still hidden.
	var readyMu sync.Mutex
	var pageReady, navDone bool
	ready := func(page, nav bool) {
		readyMu.Lock()
		pageReady, navDone = pageReady || page, navDone || nav
		both := pageReady && navDone
		readyMu.Unlock()
		if both {
			show()
		}
	}
	var upOnce sync.Once
	bridge.Ready = func() {
		// the page is up, so WebView2 started: the core counts this start
		// as a good one
		upOnce.Do(func() {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), helloTimeout)
				defer cancel()
				_, _ = client.Call(ctx, "ui.ready", nil)
			}()
		})
		ready(true, false)
	}

	a := application.New(application.Options{
		Name:        "EDSense",
		Description: "EDSense",
		Icon:        assets.IconActive,
		Logger:      logger,
		LogLevel:    slog.LevelWarn,
		Services:    []application.Service{application.NewService(bridge)},
		BindAliases: map[uint32]uint32{callAlias: bridgeCallID()},
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(web.FS()),
			Middleware:     Guard,
			DisableLogging: true,
		},
		DisableDefaultSignalHandler: true,
		ErrorHandler:                func(err error) { logger.Error(err.Error()) },
		WarningHandler:              func(msg string) { logger.Warn(msg) },
		Windows: application.WindowsOptions{
			WndClass:            "EDSense.Window",
			WebviewUserDataPath: dataDir,
			WndProcInterceptor: func(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
				if msg == w32.WM_SETTINGCHANGE {
					// a new Text size comes with a settings change
					go func() {
						if n := textScale(); int32(n) != scale.Swap(int32(n)) {
							if a := app.Load(); a != nil {
								a.Event.Emit("edsense:text", n)
							}
						}
					}()
				}
				return 0, false
			},
		},
	})
	app.Store(a)

	// Never SystemDefault: Wails would then follow Windows by itself, over
	// the player's choice. The theme change below follows it instead.
	dark := isDark(bridge.lastTheme())
	theme := application.Light
	if dark {
		theme = application.Dark
	}
	w := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                       "main",
		Title:                      "EDSense",
		Width:                      DefaultWidth,
		Height:                     DefaultHeight,
		MinWidth:                   MinWidth,
		MinHeight:                  MinHeight,
		URL:                        "/",
		Hidden:                     true,
		InitialPosition:            application.WindowCentered,
		Frameless:                  true, // the page draws the title bar
		BackgroundType:             application.BackgroundTypeSolid,
		BackgroundColour:           background(dark),
		DefaultContextMenuDisabled: true,
		Permissions:                denyAll(),
		Windows: application.WindowsWindow{
			Theme:       theme,
			CustomTheme: caption(),
			Permissions: denyAllKinds(),
			DisableMenu: true,
			// the page's app-region: drag areas move the window as its title bar
			NonClientRegionSupport: true,
		},
	})
	win.Store(w)

	w.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) { ready(false, true) })

	// System follows Windows' light and dark mode; the page does so itself.
	a.Event.OnApplicationEvent(events.Common.ThemeChanged, func(e *application.ApplicationEvent) {
		if bridge.lastTheme() == control.ThemeSystem {
			paint(w, e.Context().IsDarkMode())
		}
	})

	// The core shows messages in a box once the window is going, and a
	// click on the tray icon then waits for the next window.
	w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_, _ = client.Call(ctx, "ui.closing", nil)
	})

	// Remember the placement as it changes: on a quit from the core the
	// window is gone before any closing hook could ask. Until the window
	// shows, the saved one stands.
	var mu sync.Mutex
	normal := application.Rect{X: place.X, Y: place.Y, Width: place.Width, Height: place.Height}
	maximised := place.Maximized
	track = func() {
		if !shown.Load() {
			return
		}
		isMax, isMin := w.IsMaximised(), w.IsMinimised()
		var b application.Rect
		if !isMax && !isMin {
			b = w.PhysicalBounds()
		}
		mu.Lock()
		if !isMin {
			maximised = isMax
		}
		if b.Width > 0 && b.Height > 0 {
			normal = b
		}
		mu.Unlock()
	}
	for _, ev := range []events.WindowEventType{
		events.Common.WindowDidMove, events.Common.WindowDidResize,
		events.Common.WindowMaximise, events.Common.WindowUnMaximise, events.Common.WindowRestore,
	} {
		w.OnWindowEvent(ev, func(*application.WindowEvent) { track() })
	}
	// the page's maximize button turns into restore
	for _, ev := range []events.WindowEventType{
		events.Common.WindowMaximise, events.Common.WindowUnMaximise, events.Common.WindowRestore,
	} {
		w.OnWindowEvent(ev, func(*application.WindowEvent) {
			a.Event.Emit("edsense:win", map[string]bool{"max": w.IsMaximised()})
		})
	}
	bridge.Minimise = func() { w.Minimise() }
	bridge.ToggleMaximise = w.ToggleMaximise
	bridge.CloseWindow = w.Close

	a.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		// show anyway if the page never says it is ready
		time.AfterFunc(2*time.Second, show)
		go func() {
			<-gone
			a.Quit()
		}()
	})

	// save hands ui_state.json's content to the core, one at a time so the
	// last one written is the newest.
	var saveMu sync.Mutex
	save := func(timeout time.Duration) error {
		saveMu.Lock()
		defer saveMu.Unlock()
		mu.Lock()
		state := control.UIState{X: normal.X, Y: normal.Y, Width: normal.Width, Height: normal.Height,
			Maximized: maximised, Page: bridge.lastPage(), Theme: bridge.lastTheme()}
		mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_, err := client.Call(ctx, "ui.state", state.Valid())
		return err
	}

	// A picked theme shows at once and is saved at once, so it outlives
	// a window that is ended.
	bridge.Appearance = func(t string) {
		paint(w, isDark(t))
		go func() {
			if err := save(helloTimeout); err != nil {
				logger.Warn("theme not saved", "err", err)
			}
		}()
	}

	if err := a.Run(); err != nil {
		logger.Error("window", "err", err)
		return exitFailed
	}

	if err := save(2 * time.Second); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("placement not saved", "err", err)
	}
	conn.Flush(time.Second)
	return exitOK
}

// isDark: the window shows dark for theme.
func isDark(theme string) bool {
	switch theme {
	case control.ThemeDark:
		return true
	case control.ThemeLight:
		return false
	}
	return w32.IsCurrentlyDarkMode()
}

// paint turns the open window light or dark: the background behind the
// page, and on Windows 11 the title bar, its text and border. Wails does
// this only when the window is made.
func paint(w *application.WebviewWindow, dark bool) {
	w.SetBackgroundColour(background(dark))
	application.InvokeAsync(func() {
		hwnd := uintptr(w.NativeWindow())
		if hwnd == 0 || w32.IsCurrentlyHighContrastMode() || !w32.SupportsThemes() {
			return
		}
		w32.SetTheme(hwnd, dark)
		t := captionLight
		if dark {
			t = captionDark
		}
		w32.SetTitleBarColour(hwnd, t.bar)
		w32.SetTitleTextColour(hwnd, t.text)
		w32.SetBorderColour(hwnd, t.border)
		w32.RedrawWindow(w32.HWND(hwnd), nil, 0, w32.RDW_FRAME|w32.RDW_INVALIDATE|w32.RDW_UPDATENOW)
	})
}

// textScale is Windows' Text size (Settings, Accessibility), 100 to 225
// percent. The page scales its text by it.
func textScale() int {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Accessibility`, registry.QUERY_VALUE)
	if err != nil {
		return 100
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("TextScaleFactor")
	if err != nil || v < 100 || v > 225 {
		return 100
	}
	return int(v)
}

// systemDLLsOnly makes a DLL named without a path load from System32 only.
// By default Windows looks in EDSense's own folder first, which the player
// can write to, and Wails loads winbrand.dll and dwmapi.dll by name, as
// the WebView2 Runtime's DLL does dwmapi.dll and dbghelp.dll. That DLL is
// loaded by its full path and needs nothing outside System32.
func systemDLLsOnly() error {
	return windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
}

// fromCore: stdin is a pipe, as the core starts the window.
func fromCore(f *os.File) bool {
	t, err := windows.GetFileType(windows.Handle(f.Fd()))
	return err == nil && t == windows.FILE_TYPE_PIPE
}

// startCore starts EDSense as the tray app, which opens its window when
// it already runs.
func startCore() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	allowFrontAny()
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}

var procAllowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// allowFrontAny: the window is in front, and what the core opens next (a
// file in Notepad, a link in the browser) may come in front of it.
func allowFrontAny() {
	const asfwAny = ^uint32(0)
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(asfwAny))
}

func background(dark bool) application.RGBA {
	if dark {
		return bgDark
	}
	return bgLight
}

// The Windows 11 title bar, tinted like the page: background, muted text,
// hairline border. Colours are 0x00BBGGRR. The window is frameless, so
// of these only the border shows; the page draws the title bar.
type captionColours struct{ bar, text, border uint32 }

var (
	captionLight = captionColours{bar: 0xF3F6F7, text: 0x555B5E, border: 0xD9E0E3}
	captionDark  = captionColours{bar: 0x1A1B1C, text: 0xA7AFB3, border: 0x2F3234}
)

// caption is the title bar for Wails. It applies it only when the window
// is made, so active and inactive are the same.
func caption() application.ThemeSettings {
	theme := func(c captionColours) *application.WindowTheme {
		return &application.WindowTheme{TitleBarColour: &c.bar, TitleTextColour: &c.text, BorderColour: &c.border}
	}
	light, dark := theme(captionLight), theme(captionDark)
	return application.ThemeSettings{
		LightModeActive: light, LightModeInactive: light,
		DarkModeActive: dark, DarkModeInactive: dark,
	}
}

// denyAll covers the cross-platform permission types.
func denyAll() map[application.PermissionType]application.Permission {
	m := map[application.PermissionType]application.Permission{}
	for _, p := range []application.PermissionType{
		application.PermissionMicrophone, application.PermissionCamera, application.PermissionGeolocation,
		application.PermissionNotifications, application.PermissionClipboardRead,
	} {
		m[p] = application.PermissionDeny
	}
	return m
}

// denyAllKinds covers every WebView2 permission kind, including the ones
// Wails has no name for (sensors, downloads, file access, autoplay, local
// fonts, MIDI, window management). Without any entry Wails would allow
// them all, and a kind missing from the map would get the WebView2 prompt.
func denyAllKinds() map[application.CoreWebView2PermissionKind]application.CoreWebView2PermissionState {
	m := map[application.CoreWebView2PermissionKind]application.CoreWebView2PermissionState{}
	for k := 0; k < 32; k++ {
		m[application.CoreWebView2PermissionKind(k)] = application.CoreWebView2PermissionStateDeny
	}
	return m
}

// userDataPath is %LOCALAPPDATA%\EDSense\WebView2, with -admin when
// elevated so an elevated run never shares the folder with a normal one.
func userDataPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	name := "WebView2"
	if windows.GetCurrentProcessToken().IsElevated() {
		name += "-admin"
	}
	return filepath.Join(base, "EDSense", name), nil
}

// placeWindow applies the saved placement if it is still on an attached
// monitor, clamped to that monitor's work area. Otherwise the window gets
// the default size, shrunk to fit the primary work area, centred there.
// Physical pixels throughout: Wails computes its own DIP layout across
// monitors, so physical is the safer unit to save.
func placeWindow(a *application.App, w *application.WebviewWindow, st control.UIState) {
	if st.Width > 0 && st.Height > 0 {
		saved := application.Rect{X: st.X, Y: st.Y, Width: st.Width, Height: st.Height}
		var best *application.Screen
		bestArea := 0
		for _, s := range a.Screen.GetAll() {
			if area := overlap(saved, s.PhysicalWorkArea); area > bestArea {
				best, bestArea = s, area
			}
		}
		if best != nil {
			w.SetPhysicalBounds(clamp(saved, best.PhysicalWorkArea))
			return
		}
	}
	if s := a.Screen.GetPrimary(); s != nil {
		width, height := min(DefaultWidth, s.WorkArea.Width), min(DefaultHeight, s.WorkArea.Height)
		w.SetSize(max(width, MinWidth), max(height, MinHeight))
		w.SetScreen(s) // centres on the work area without activating
	}
}

func overlap(a, b application.Rect) int {
	w := min(a.X+a.Width, b.X+b.Width) - max(a.X, b.X)
	h := min(a.Y+a.Height, b.Y+b.Height) - max(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func clamp(r, area application.Rect) application.Rect {
	r.Width = min(r.Width, area.Width)
	r.Height = min(r.Height, area.Height)
	r.X = min(max(r.X, area.X), area.X+area.Width-r.Width)
	r.Y = min(max(r.Y, area.Y), area.Y+area.Height-r.Height)
	return r
}
