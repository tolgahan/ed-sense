//go:build !windows

package window

import (
	"fmt"
	"os"
)

// Run needs Windows: the window is a WebView2 page.
func Run(Options) int {
	fmt.Fprintln(os.Stderr, "the window needs Windows")
	return exitFailed
}
