package gyro

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBiasFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), BiasFile)
	if _, ok := LoadBias(path); ok {
		t.Error("a missing file loaded")
	}
	want := [3]float64{0.1234, -0.8, 2.5}
	if err := SaveBias(path, [3]float64{0.12341, -0.8, 2.5}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "{\"bias\":[0.1234,-0.8,2.5]}\n" {
		t.Errorf("file: %q %v", b, err)
	}
	if got, ok := LoadBias(path); !ok || got != want {
		t.Errorf("round trip: %v %v, want %v", got, ok, want)
	}
	for _, broken := range []string{"", "{bias", "{}", `{"bias":[1,2]}`, `{"bias":[1,2,3,4]}`, `{"bias":"x"}`} {
		if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
			t.Fatal(err)
		}
		if b, ok := LoadBias(path); ok {
			t.Errorf("%q loaded as %v", broken, b)
		}
	}
}
