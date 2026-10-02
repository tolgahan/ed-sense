package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/config"
)

// TestCatalogCoversSchema: js/catalog.js has words for every setting the
// window shows, every key of the maps with fixed keys, every trigger
// mode, weapon type and haptics mode.
func TestCatalogCoversSchema(t *testing.T) {
	b, err := fs.ReadFile(FS(), "js/catalog.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	var missing []string
	want := func(s string) {
		if !strings.Contains(src, s) {
			missing = append(missing, s)
		}
	}
	closed := map[string]bool{"haptics_gain": true, "rumble": true, "colors": true, "triggers": true, "spin_up_ms": true, "hud_colors": true}
	rows := 0
	for _, k := range config.Schema() {
		name := strings.TrimSuffix(k.Path, ".*")
		if name == "config_version" {
			continue
		}
		rows++
		want(`"` + name + `":`)
		if closed[name] {
			if len(k.Keys) == 0 {
				t.Errorf("%s: no keys in the Schema", k.Path)
			}
			for _, e := range k.Keys {
				want(`"` + e + `"`)
			}
		}
		switch name {
		case "triggers":
			for _, m := range k.Modes {
				want(`"` + m.Name + `"`)
			}
		case "fire_groups", "haptics_mode":
			for _, e := range k.Enum {
				want(`"` + e + `"`)
			}
		}
	}
	if rows == 0 {
		t.Error("no settings in the Schema")
	}
	if len(missing) > 0 {
		t.Errorf("js/catalog.js lacks %d: %s", len(missing), strings.Join(missing, " "))
	}
}

// TestStylesLinked: index.html links app.css first, then each page's
// styles in css/ once, and links no file the page lacks.
func TestStylesLinked(t *testing.T) {
	page, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	var links []string
	for _, m := range regexp.MustCompile(`<link rel="stylesheet" href="/([^"]+)">`).FindAllStringSubmatch(string(page), -1) {
		links = append(links, m[1])
	}
	if len(links) == 0 || links[0] != "app.css" {
		t.Fatalf("stylesheets %v: app.css should come first", links)
	}
	seen := map[string]int{}
	for _, l := range links {
		seen[l]++
		if _, err := fs.Stat(FS(), l); err != nil {
			t.Errorf("index.html links %s, which is not there", l)
		}
	}
	css, err := fs.Glob(FS(), "css/*.css")
	if err != nil || len(css) == 0 {
		t.Fatalf("no page styles in css/: %v", err)
	}
	for _, f := range css {
		if seen[f] != 1 {
			t.Errorf("index.html links %s %d times", f, seen[f])
		}
	}
	for _, p := range []string{"feel", "triggers", "lights", "gyro", "hud", "advanced"} {
		if seen["css/"+p+".css"] == 0 {
			t.Errorf("no css/%s.css", p)
		}
	}
}
