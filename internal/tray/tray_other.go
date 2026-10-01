//go:build !windows

// Package tray runs EDSense from the notification area. Elsewhere than on
// Windows there is no tray: EDSense runs until interrupted.
package tray

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/engine"
)

// Options are the tray's files and words.
type Options struct {
	CfgPath, LogPath, Version string
	TrayOnly                  bool // started with -tray: no window at start
}

func Run(a *app.App, eng *engine.Engine, store *config.Store, o Options) {
	stop := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		close(stop)
	}()
	eng.Run(stop)
}
