package main

import (
	"io"
	"log"
	"os"
	"runtime/debug"
)

// maxLogSize: a bigger log is started afresh.
const maxLogSize = 1 << 20

// setUpLog writes the log to path, and also to the console when asked.
func setUpLog(path string, console bool) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogSize {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	switch {
	case err != nil && console:
		log.SetOutput(os.Stdout)
	case err != nil:
		log.SetOutput(io.Discard)
	case console:
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	default:
		log.SetOutput(f)
	}
	if err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{}) // crashes in any goroutine end up in the log
	}
}
