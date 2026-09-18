// EDSense gives Elite Dangerous a DualSense feel through DSX: adaptive
// triggers, lightbar and LEDs that follow the game.
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
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/platform"
	"github.com/tolgahan/ed-sense/internal/tray"
)

// version is set when building: -ldflags "-X main.version=1.0.0".
var version = "dev"

const name = "EDSense"

func main() {
	console := flag.Bool("console", false, "run in this console instead of the tray")
	verbose := flag.Bool("verbose", false, "print every packet sent to DSX")
	cfgPath := flag.String("config", "", "settings file (default: edsense.json in the data folder)")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()

	// The Windows build has no console of its own: command-line modes use
	// the one they were started from.
	cli := *console || *verbose || *showVersion
	if cli {
		platform.AttachConsole()
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
	logPath := filepath.Join(dataDir, "edsense.log")
	setUpLog(logPath, cli)
	log.Printf("%s %s", name, version)
	cfg, err := config.Load(path)
	if err != nil {
		log.Printf("Settings error, using the defaults: %v", err)
	}

	port := dsx.Port(cfg.DSXPort)
	client, err := dsx.NewClient(port, *verbose)
	if err != nil {
		log.Printf("Cannot open the UDP socket: %v", err)
		platform.ShowError(name, "EDSense could not open its network socket:\n"+err.Error())
		os.Exit(1)
	}
	defer client.Close()
	log.Printf("DSX UDP port %d, settings %s", port, path)

	a := app.New(path, &cfg, client)
	if !cli {
		tray.Run(a, path, logPath, version)
		return
	}
	a.Run(interrupted()) // Ctrl+C hands the controller back to DSX first
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
