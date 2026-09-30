//go:build !windows

package ds4w

// ExeVersion needs Windows.
func ExeVersion(path string) string { return "" }
