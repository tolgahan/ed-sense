package hud

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

const testOverrideXML = `<?xml version="1.0" encoding="UTF-8" ?>
<GraphicsConfig>
  <GUIColour>
    <Default>
      <LocalisationName>Standard</LocalisationName>
      <!-- old preset:
      <MatrixRed> 1, 0, 0 </MatrixRed>
      <MatrixGreen> 0, 1, 0 </MatrixGreen>
      <MatrixBlue> 0, 0, 1 </MatrixBlue>
      -->
      <MatrixRed> 0, 0.39, 1 </MatrixRed>
      <MatrixGreen> 1, 1, 0 </MatrixGreen>
      <MatrixBlue> 0, 0, 0 </MatrixBlue>
    </Default>
  </GUIColour>
</GraphicsConfig>
`

var icyMatrix = [3][3]float64{{0, 0.39, 1}, {1, 1, 0}, {0, 0, 0}}

// writeOverride points LOCALAPPDATA at a folder with the given override file.
func writeOverride(t *testing.T, xml string) {
	local := t.TempDir()
	dir := filepath.Join(local, "Frontier Developments", "Elite Dangerous", "Options", "Graphics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "GraphicsConfigurationOverride.xml"), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", local)
}

func TestColourMatrix(t *testing.T) {
	writeOverride(t, testOverrideXML)
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Frontier Developments", "Elite Dangerous", "Options", "Graphics")
	m, ok := colourMatrix(filepath.Join(dir, "GraphicsConfigurationOverride.xml"))
	if !ok || m != icyMatrix {
		t.Fatalf("matrix %v %v, want %v (the commented preset must be skipped)", m, ok, icyMatrix)
	}
	if isIdentity(m) || !isIdentity([3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}) {
		t.Fatal("isIdentity")
	}
	p := DefaultPalette.underMatrix(icyMatrix)
	if p.Shield != (vision.Color{200, 216, 41}) || p.Heat != (vision.Color{108, 181, 186}) {
		t.Fatalf("predicted palette %v", p)
	}
	if _, ok := colourMatrix(filepath.Join(t.TempDir(), "missing.xml")); ok {
		t.Fatal("a missing file read as a matrix")
	}
}

func TestResolvePalette(t *testing.T) {
	writeOverride(t, testOverrideXML)
	data := t.TempDir()
	predicted := vision.Color{200, 216, 41}

	pal, source, key := ResolvePalette(nil, data)
	if pal.Shield != predicted {
		t.Fatalf("matrix prediction not used: %v (%s)", pal, source)
	}
	if pal.NoFlash != (vision.ColorDistance(pal.Shield, pal.Flash) < 0.35) {
		t.Fatal("flash check")
	}

	// learned colours win over the prediction, for the same matrix only
	if err := SaveLearned(data, key, Palette{Shield: vision.Color{1, 2, 3}, Heat: vision.Color{4, 5, 6}}); err != nil {
		t.Fatal(err)
	}
	if pal, _, _ = ResolvePalette(nil, data); pal.Shield != (vision.Color{1, 2, 3}) || pal.Heat != (vision.Color{4, 5, 6}) {
		t.Fatalf("learned palette not used: %v", pal)
	}
	if err := SaveLearned(data, "other matrix", Palette{Shield: vision.Color{9, 9, 9}}); err != nil {
		t.Fatal(err)
	}
	if pal, _, _ = ResolvePalette(nil, data); pal.Shield != predicted {
		t.Fatalf("learned palette of another matrix used: %v", pal)
	}

	// hud_colors win over everything, and stop learning
	manual := map[string]string{"Shield": "#102030", "flash": "off"}
	if pal, source, _ = ResolvePalette(manual, data); pal.Shield != (vision.Color{0x10, 0x20, 0x30}) || !pal.NoFlash {
		t.Fatalf("manual colours not used: %v (%s)", pal, source)
	}
	if !ManualColours(manual) {
		t.Fatal("ManualColours")
	}
	if ManualColours(map[string]string{"flash": "off"}) {
		t.Fatal("flash off alone should not stop learning")
	}

	// no override file: the standard colours
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if pal, _, _ = ResolvePalette(nil, t.TempDir()); pal.Shield != DefaultPalette.Shield || pal.NoFlash {
		t.Fatalf("standard palette %v", pal)
	}
}
