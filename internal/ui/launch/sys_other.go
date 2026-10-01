//go:build !windows

package launch

import (
	"io"
	"os"
)

// The window needs Windows; elsewhere only the tests start a process.

func allowForeground(pid int)           {}
func AllowForegroundAny()               {}
func noInherit(f *os.File)              {}
func newJob(pid int) (io.Closer, error) { return nil, nil }

func RuntimeMissing() (missing bool, found string) { return false, "" }
func ListenForOpen(open func()) (func(), error)    { return func() {}, nil }
func SignalRunning() bool                          { return false }
