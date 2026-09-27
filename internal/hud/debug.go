package hud

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

type debugShot struct {
	name string
	img  *vision.Image
}

// debugFilesKept: the newest files kept in the debug folder.
const debugFilesKept = 600

// saveDebug writes a tick's captures, named after what was read in them.
func saveDebug(dir string, now time.Time, r Reading, shots []debugShot) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	stamp := now.Format("20060102-150405.000")
	for i, s := range shots {
		name := fmt.Sprintf("%s_%d_%s.png", stamp, i, debugName(s.name, r))
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		_ = png.Encode(f, s.img.ToRGBA())
		f.Close()
	}
	pruneDebug(dir)
}

func debugName(shot string, r Reading) string {
	switch shot {
	case "ship":
		name := fmt.Sprintf("ship_shield-%s-%.2f_hull-%s", fileSafe(r.Shield.Text), r.Shield.Score, fileSafe(r.Hull.Text))
		if r.CapsOK {
			name += fmt.Sprintf("_caps-%.0f-%.0f-%.0f", r.Caps[SYS]*100, r.Caps[ENG]*100, r.Caps[WEP]*100)
		}
		return name
	case "heat":
		return fmt.Sprintf("heat-%s-%.2f", fileSafe(r.Heat.Text), r.Heat.Score)
	case "throttle":
		zone := "none"
		if r.BlueZoneSeen {
			zone = "out"
			if r.InBlueZone {
				zone = "in"
			}
		}
		return "throttle-blue-" + zone
	case "target":
		return fmt.Sprintf("target_shield-%s_hull-%s_flash-%.1f", fileSafe(r.Target.Shield.Text), fileSafe(r.Target.Hull.Text), r.Target.Splash)
	}
	return shot
}

// fileSafe: "85%" becomes "85p".
func fileSafe(s string) string {
	if s == "" {
		return "none"
	}
	return strings.ReplaceAll(s, "%", "p")
}

// pruneDebug removes the oldest captures, by the time they were written:
// the names of older versions don't sort by time.
func pruneDebug(dir string) {
	entries, _ := os.ReadDir(dir)
	if len(entries) <= debugFilesKept {
		return
	}
	type file struct {
		name    string
		written time.Time
	}
	files := make([]file, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			files = append(files, file{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].written.Equal(files[j].written) {
			return files[i].written.Before(files[j].written)
		}
		return files[i].name < files[j].name
	})
	for _, f := range files[:max(0, len(files)-debugFilesKept)] {
		_ = os.Remove(filepath.Join(dir, f.name))
	}
}
