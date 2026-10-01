package ds4w

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/tolgahan/ed-sense/internal/platform"
)

func closedWith(n ClosedNames, exeDir string) (bool, string) {
	if platform.ProcessRunning(n.Exe) {
		return false, n.Exe + " runs"
	}
	if exe := customExe(exeDir, os.ReadFile); exe != "" && !strings.EqualFold(exe, n.Exe) && platform.ProcessRunning(exe) {
		return false, exe + " runs"
	}
	if _, ok := WindowProcess(n.IPC); ok {
		return false, "its window is open"
	}
	return eventGone(n.Event)
}

// eventGone: the single-instance event does not exist. A handle that
// opens is closed at once: holding it would keep the next DS4Windows from
// starting, and only SYNCHRONIZE is asked for, which cannot set it.
func eventGone(name string) (bool, string) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, "its event name is not valid"
	}
	h, err := windows.OpenEvent(windows.SYNCHRONIZE, false, p)
	if err == nil {
		windows.CloseHandle(h)
		return false, "it runs"
	}
	switch {
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND):
		return true, ""
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return false, "it runs as administrator"
	}
	return false, "it could not be checked (" + err.Error() + ")"
}
