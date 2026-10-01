//go:build !windows

package ds4w

// closedWith needs Windows; elsewhere DS4Windows counts as running.
func closedWith(n ClosedNames, exeDir string) (bool, string) {
	return false, "checking needs Windows"
}
