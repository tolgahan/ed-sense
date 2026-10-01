package window

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// TestSystemDLLsOnly: a DLL next to the exe with a System32 name is not
// the one a load by name gets, as Wails loads winbrand.dll.
func TestSystemDLLsOnly(t *testing.T) {
	const name = "winbrand.dll"
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(sys, name))
	if err != nil {
		t.Skip(name, "is not in System32 here")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plant := filepath.Join(filepath.Dir(exe), name)
	if err := os.WriteFile(plant, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(plant) })

	if err := systemDLLsOnly(); err != nil {
		t.Fatal(err)
	}
	h, err := windows.LoadLibrary(name) // by name, as syscall.NewLazyDLL does
	if err != nil {
		t.Fatal(err)
	}
	defer windows.FreeLibrary(h)
	var buf [windows.MAX_PATH]uint16
	n, err := windows.GetModuleFileName(h, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Fatal(err)
	}
	if got := windows.UTF16ToString(buf[:n]); !strings.EqualFold(filepath.Dir(got), sys) {
		t.Errorf("%s came from %s", name, got)
	}
}

// TestCaption: the title bar Wails paints first is the one a theme switch
// paints later.
func TestCaption(t *testing.T) {
	c := caption()
	for _, tc := range []struct {
		name string
		got  *application.WindowTheme
		want captionColours
	}{
		{"light", c.LightModeActive, captionLight},
		{"light inactive", c.LightModeInactive, captionLight},
		{"dark", c.DarkModeActive, captionDark},
		{"dark inactive", c.DarkModeInactive, captionDark},
	} {
		got := captionColours{bar: *tc.got.TitleBarColour, text: *tc.got.TitleTextColour, border: *tc.got.BorderColour}
		if got != tc.want {
			t.Errorf("%s: %06X, want %06X", tc.name, got, tc.want)
		}
	}
}
