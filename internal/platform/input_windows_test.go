package platform

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestInputBlocked(t *testing.T) {
	own, err := tokenIntegrity(windows.GetCurrentProcessToken())
	if err != nil || own < 0x1000 {
		t.Fatalf("own integrity level %#x, %v", own, err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if InputBlocked(filepath.Base(exe)) {
		t.Error("a program at our own level counts as blocked")
	}
	if InputBlocked("no-such-program.exe") {
		t.Error("a program that is not running counts as blocked")
	}
}
