// EDSense gives Elite Dangerous a DualSense feel through DSX or DS4Windows:
// adaptive triggers, lightbar and LEDs that follow the game, and haptics
// for what happens in the cockpit.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/diag"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/platform"
	"github.com/tolgahan/ed-sense/internal/tray"
	"github.com/tolgahan/ed-sense/internal/ui/launch"
	"github.com/tolgahan/ed-sense/internal/ui/window"
)

// version is set when building: -ldflags "-X main.version=1.0.0".
var version = "dev"

const name = "EDSense"

func main() {
	demo := flag.Bool("demo", false, "play every effect once, without Elite running")
	padTest := flag.Bool("padtest", false, "find the virtual DualSense, test each side (rumble with DSX, native haptics with DS4Windows) and show its input")
	hapticsTest := flag.Bool("hapticstest", false, "play native haptics through the virtual DualSense's audio device")
	feelTest := flag.Bool("feeltest", false, "play the turn and jump feels one after another, to choose turn_feel and jump_feel")
	gyroTest := flag.Bool("gyrotest", false, "measure DSX's gyro aim against EDSense's, and calibrate the gyro")
	hudTest := flag.Bool("hudtest", false, "read the HUD from the screenshots given as arguments (PNG, JPEG or BMP)")
	console := flag.Bool("console", false, "run in this console instead of the tray")
	verbose := flag.Bool("verbose", false, "print every packet sent to DSX or DS4Windows")
	backendFlag := flag.String("backend", "", "the controller app for this run: auto, dsx or ds4windows (not saved)")
	cfgPath := flag.String("config", "", "settings file (default: edsense.json in the data folder)")
	showVersion := flag.Bool("version", false, "print the version")
	trayOnly := flag.Bool("tray", false, "start in the tray without opening the window, as from the Startup folder")
	asWindow := flag.Bool("window", false, "run as EDSense's window (EDSense starts it itself)")
	flag.Parse()
	platform.MakeDPIAware()
	if *asWindow {
		// before the log, the settings, the socket and the mutex: those
		// are the tray EDSense's
		os.Exit(window.Run(window.Options{Version: version}))
	}
	launch.ClearWebViewEnv()

	// The Windows build has no console of its own: command-line modes use
	// the one they were started from.
	cli := *demo || *padTest || *hapticsTest || *feelTest || *gyroTest || *hudTest || *console || *verbose || *showVersion
	switch {
	case cli:
		platform.AttachConsole()
	case *trayOnly:
		if platform.InstanceRunning(platform.InstanceMutex) {
			return // it is in the tray already
		}
	case launch.SignalRunning():
		return // a second start: the running EDSense opens its window
	}
	if *showVersion {
		fmt.Println(name, version)
		return
	}
	path := *cfgPath
	if path == "" {
		path = filepath.Join(platform.DataDir(name), "edsense.json")
	}
	dataDir := filepath.Dir(path)
	if *hudTest {
		cfg, _ := config.Load(path)
		diag.HUD(flag.Args(), &cfg, dataDir)
		return
	}
	logPath := filepath.Join(dataDir, "edsense.log")
	setUpLog(logPath, cli)
	log.Printf("%s %s", name, version)
	cfg, err := config.Load(path)
	if err != nil {
		log.Printf("Settings error, using the defaults: %v", err)
	}

	port := dsx.Port(cfg.DSXPort)
	ds4Addr, ds4Dir := backend.DS4WindowsListener(cfg.DS4WindowsPort)
	choice := cfg.BackendChoice()
	if *backendFlag != "" {
		choice = *backendFlag
	}
	env := backend.DetectEnv{
		Running:    platform.ProcessRunning,
		DS4Window:  backend.DS4WindowsRunning,
		DS4Version: backend.DS4WindowsVersion,
		Probe:      func(addr *net.UDPAddr) (dsx.Dialect, bool) { return dsx.Probe(addr, backend.ProbeTimeout) },
		DSXAddr:    &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port},
		DS4Addr:    ds4Addr,
	}
	var watch *backend.AppWatch
	kind, why := backend.Kind(choice), "set in "+filepath.Base(path)
	if *backendFlag != "" {
		why = "-backend"
	}
	switch kind {
	case backend.KindDSX, backend.KindDS4Windows:
	default:
		if choice != config.BackendAuto {
			log.Printf("Unknown controller app %q, picking one", choice)
		}
		watch = &backend.AppWatch{Env: env}
		kind, why, _ = watch.Check()
		why = "auto: " + why
	}
	var ctl *backend.Backend
	if kind == backend.KindDS4Windows {
		ctl, err = backend.DS4Windows(backend.DS4WindowsOptions{Addr: ds4Addr, Port: cfg.DS4WindowsPort, Follow: true,
			Verbose: *verbose, Haptics: func() string { return cfg.DS4WindowsHaptics }})
	} else {
		ctl, err = backend.DSX(port, *verbose)
	}
	if err != nil {
		log.Printf("Cannot open the UDP socket: %v", err)
		platform.ShowError(name, "EDSense could not open its network socket:\n"+err.Error())
		os.Exit(1)
	}
	defer ctl.Close()
	log.Printf("Controller app: %s (%s)", ctl.Name, why)
	if kind == backend.KindDS4Windows {
		log.Printf("DS4Windows UDP %s (%s), settings %s", ds4Addr, backend.DS4WindowsSettingsFrom(ds4Dir), path)
	} else {
		log.Printf("DSX UDP port %d, settings %s", port, path)
	}

	a := app.New(path, &cfg, ctl)
	if watch != nil {
		a.WatchApps(watch.Check)
	}
	if !cli {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		if kind == backend.KindDS4Windows {
			addr = ds4Addr.String()
		}
		tray.Run(a, tray.Options{CfgPath: path, LogPath: logPath, Version: version, Addr: addr, TrayOnly: *trayOnly})
		return
	}
	done := interrupted() // Ctrl+C hands the controller back to the backend first
	switch {
	case *padTest:
		diag.Rumble(ctl, done)
	case *hapticsTest:
		diag.Haptics(ctl, done)
	case *feelTest, *gyroTest:
		if platform.InstanceRunning(platform.InstanceMutex) && platform.ProcessRunning(elite.GameExe) {
			fmt.Println("EDSense is running in the tray while Elite runs, and its effects would mix into this test.")
			fmt.Println("Close Elite, or quit EDSense from its tray menu, and run this again.")
			return
		}
		if *gyroTest {
			diag.Gyro(ctl, cfg, dataDir, done)
			return
		}
		diag.Feel(ctl, cfg, done)
	case *demo:
		a.PlayDemo(done)
	default:
		a.Run(done)
	}
}

func interrupted() <-chan struct{} {
	done := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		close(done)
	}()
	return done
}
