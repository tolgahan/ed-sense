package window

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/ui/web"
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

// TestAssetsServed: every page file, the page styles in css/ too, comes
// through the window's handler with its type.
func TestAssetsServed(t *testing.T) {
	types := map[string]string{".html": "text/html", ".css": "text/css", ".js": "text/javascript"}
	h := Guard(application.AssetFileServerFS(web.FS()))
	n := 0
	err := fs.WalkDir(web.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, ok := types[path.Ext(p)]
		if !ok {
			return nil
		}
		n++
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", Origin+"/"+p, nil))
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), want) {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Content-Type"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	css, _ := fs.Glob(web.FS(), "css/*.css")
	if n < 5 || len(css) == 0 {
		t.Errorf("served %d files, %d page styles", n, len(css))
	}
}
