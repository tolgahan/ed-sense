//go:build !windows

// Package tray runs EDSense from the notification area. Elsewhere than on
// Windows there is no tray: EDSense runs until interrupted.
package tray

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/tolgahan/ed-sense/internal/app"
)

func Run(a *app.App, cfgPath, logPath, version string) {
	stop := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		close(stop)
	}()
	a.Run(stop)
}
