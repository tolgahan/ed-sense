package platform

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// AutostartEnabled reports whether the app starts with Windows.
func AutostartEnabled(app string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(app)
	return err == nil
}

// AutostartMoved reports whether the app starts with Windows from another
// copy of the exe (it was moved since).
func AutostartMoved(app string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	cmd, _, err := k.GetStringValue(app)
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	return err == nil && !strings.EqualFold(cmd, `"`+exe+`"`)
}

// SetAutostart makes this exe start with Windows, or not.
func SetAutostart(app string, on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		return k.SetStringValue(app, `"`+exe+`"`)
	}
	if err := k.DeleteValue(app); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
