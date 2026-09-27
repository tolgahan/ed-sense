package hud

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// Recorded gameplay is not in the repository. The data tests read it from:
//
//	HUD_FRAMES: frames of a recorded fight (2560x1440, compressed video), with
//	  labels_shield.json / labels_heat.json (file -> value) and golden_calib.json
//	HUD_CROPS: the reader's own captures from a 4K game window (*_0_* shield,
//	  *_1_* heat), with truth_shield.json / truth_heat.json ("?" = unreadable)
//	HUD_WPN: 4K fire group list captures (*_weaponsL-*.png, *_weaponsR-*.png)
//	HUD_SEQ: a 10 fps frame sequence

var testStart = time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)

func dataDir(t *testing.T, env string) string {
	dir := os.Getenv(env)
	if dir == "" {
		t.Skip(env + " not set")
	}
	return dir
}

func loadImage(t testing.TB, path string) *vision.Image {
	im, err := vision.LoadImage(path)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

// loadLabels returns the labels and the names of the readable ones, sorted.
func loadLabels(t testing.TB, path string) (map[string]string, []string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	var labels map[string]string
	if err := json.Unmarshal(raw, &labels); err != nil {
		t.Fatal(err)
	}
	var names []string
	for n, v := range labels {
		if v != "?" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return labels, names
}

func sameValue(n Number, want string) bool {
	v, ok := n.Value()
	return ok && fmt.Sprint(v) == want
}

// recolour applies a colour matrix to the lower half of a frame, where the
// HUD panels are.
func recolour(im *vision.Image, m [3][3]float64) *vision.Image {
	out := &vision.Image{W: im.W, H: im.H, Stride: im.Stride, Pix: make([]byte, len(im.Pix))}
	for i := im.H / 2 * im.Stride; i < len(im.Pix); i += 4 {
		r, g, b := float64(im.Pix[i+2])/255, float64(im.Pix[i+1])/255, float64(im.Pix[i])/255
		for k := range 3 {
			v := math.Max(0, math.Min(1, r*m[0][k]+g*m[1][k]+b*m[2][k]))
			out.Pix[i+2-k] = byte(v*255 + 0.5)
		}
		out.Pix[i+3] = 255
	}
	return out
}

// downscale box-filters an image, like the game rendering at a lower resolution.
func downscale(im *vision.Image, k float64) *vision.Image {
	w, h := int(float64(im.W)*k), int(float64(im.H)*k)
	out := vision.NewImage(w, h)
	for y := range h {
		for x := range w {
			x0, x1 := int(float64(x)/k), int(float64(x+1)/k)
			y0, y1 := int(float64(y)/k), int(float64(y+1)/k)
			var sum [4]int
			n := 0
			for yy := y0; yy < y1 && yy < im.H; yy++ {
				for xx := x0; xx < x1 && xx < im.W; xx++ {
					for c := range 4 {
						sum[c] += int(im.Pix[yy*im.Stride+xx*4+c])
					}
					n++
				}
			}
			for c := range 4 {
				out.Pix[y*out.Stride+x*4+c] = byte(sum[c] / max(1, n))
			}
		}
	}
	return out
}

// frameGrabber serves a still image as the game window.
type frameGrabber struct{ cur *vision.Image }

func (f *frameGrabber) Window() (int, int, bool) { return f.cur.W, f.cur.H, true }

func (f *frameGrabber) Grab(r vision.Rect) (*vision.Image, error) { return f.cur.Crop(r), nil }

// kraitLoadout: the test pilot's ship when the captures were made.
func kraitLoadout() []elite.Module {
	return elite.LoadoutModules(elite.Event{"event": "Loadout", "Modules": []any{
		map[string]any{"Slot": "LargeHardpoint1", "Item": "hpt_multicannon_gimbal_large", "AmmoInClip": 77.0, "AmmoInHopper": 2100.0},
		map[string]any{"Slot": "LargeHardpoint2", "Item": "hpt_multicannon_gimbal_large", "AmmoInClip": 77.0, "AmmoInHopper": 2100.0},
		map[string]any{"Slot": "LargeHardpoint3", "Item": "hpt_multicannon_gimbal_large", "AmmoInClip": 77.0, "AmmoInHopper": 1680.0},
		map[string]any{"Slot": "MediumHardpoint1", "Item": "hpt_beamlaser_gimbal_medium"},
		map[string]any{"Slot": "MediumHardpoint2", "Item": "hpt_beamlaser_gimbal_medium"},
		map[string]any{"Slot": "TinyHardpoint1", "Item": "hpt_shieldbooster_size0_class5"},
		map[string]any{"Slot": "TinyHardpoint3", "Item": "hpt_heatsinklauncher_turret_tiny", "AmmoInClip": 1.0, "AmmoInHopper": 2.0},
		map[string]any{"Slot": "TinyHardpoint4", "Item": "hpt_heatsinklauncher_turret_tiny", "AmmoInClip": 1.0, "AmmoInHopper": 2.0},
		map[string]any{"Slot": "Slot02_Size6", "Item": "int_shieldcellbank_size6_class5", "AmmoInClip": 1.0, "AmmoInHopper": 4.0},
		map[string]any{"Slot": "Slot01_Size6", "Item": "int_shieldgenerator_size6_class3_fast"},
	}})
}
