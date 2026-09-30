//go:build !windows

package ds4w

import "errors"

// Query needs Windows.
func Query(n Names, slot int, prop string) (string, error) {
	return "", errors.New("asking DS4Windows needs Windows")
}

// WindowProcess needs Windows.
func WindowProcess(n Names) (exe string, ok bool) { return "", false }
