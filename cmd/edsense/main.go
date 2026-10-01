// EDSense gives Elite Dangerous a DualSense feel through DSX or DS4Windows:
// adaptive triggers, lightbar and LEDs that follow the game, and haptics
// for what happens in the cockpit.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/diag"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/engine"
	"github.com/tolgahan/ed-sense/internal/install"
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
		cfg, _, _ := config.Read(path) // writes nothing
		diag.HUD(flag.Args(), &cfg, dataDir)
		return
	}
	logPath := filepath.Join(dataDir, "edsense.log")
	setUpLog(logPath, cli)
	log.Printf("%s %s", name, version)
	store, info, err := config.OpenStore(path)
	logSettings(path, info, err)
	cfg := store.Snapshot().Config.Clone() // the loop's own

	eng, err := engine.New(engine.Options{Store: store, Cfg: &cfg, Flag: *backendFlag, Verbose: *verbose})
	if err != nil {
		platform.ShowError(name, "EDSense could not open its network socket:\n"+err.Error())
		os.Exit(1)
	}
	defer eng.Close()
	ctl := eng.Backend()

	a := app.New(store, &cfg, ctl)
	eng.Attach(a)
	if !cli {
		// without the WebView2 Runtime there is no first run to wait for
		missing, _ := launch.RuntimeMissing()
		svc := makeInstall(a, eng, store, dataDir, missing)
		defer svc.Close()
		tray.Run(a, eng, store, svc, tray.Options{CfgPath: path, LogPath: logPath, Version: version, TrayOnly: *trayOnly})
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
		svc := makeInstall(a, eng, store, dataDir, true) // -console has no first run
		defer svc.Close()
		svc.Start()
		eng.Run(done)
	}
}

// makeInstall makes the install service, which writes EDSense's profiles
// for Elite into DSX and DS4Windows, off the loop; its Start starts it.
// DSX's profile is added by itself only while DSX is chosen or surely
// runs, and once the first run is over (the backend is chosen), unless
// noFirstRun: then there is none to wait for.
func makeInstall(a *app.App, eng *engine.Engine, store *config.Store, dataDir string, noFirstRun bool) *install.Service {
	return install.Make(install.Options{
		DataDir: dataDir,
		Notify:  a.Tell,
		Kind:    func() string { return string(eng.State().Kind) },
		FirstAdd: func() bool {
			return eng.DSXSure() && (noFirstRun || store.Snapshot().Config.Backend != "")
		},
		Watch:  eng.Watch,
		Report: a.SetupReport,
		Port:   func() int { return store.Snapshot().Config.DS4WindowsPort },
	})
}

// logSettings says what reading the settings file found, when it is worth
// a line.
func logSettings(path string, info config.Info, err error) {
	switch {
	case info.Origin == config.Migrated && err != nil:
		log.Printf("Settings: could not save the updated file: %v", err)
	case err != nil:
		log.Printf("Settings error, using the defaults: %v", err)
	case info.From > config.Version:
		log.Printf("%s is from a newer EDSense (config_version %d): it is read, and written with this version's keys only when a setting changes",
			filepath.Base(path), info.From)
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
