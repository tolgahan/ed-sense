package platform

import (
	"errors"
	"os"

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
