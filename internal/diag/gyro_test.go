package diag

import (
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
)

// slowSetup is a backend setup whose first check ends after a while.
type slowSetup struct {
	use     backend.GyroUse
	checked chan struct{}
}

func (s *slowSetup) Step()                    {}
func (s *slowSetup) Gyro() backend.GyroUse    { return s.use }
func (s *slowSetup) Checked() <-chan struct{} { return s.checked }

// TestProfileGyro: -gyrotest waits for the backend's first profile check
// and stops under DS4Windows unless the profile leaves the gyro alone.
func TestProfileGyro(t *testing.T) {
	s := &slowSetup{use: backend.GyroUnknown, checked: make(chan struct{})}
	b := &backend.Backend{NewSetup: func(string, func(string)) backend.Setup { return s }}
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.use = backend.GyroMouse
		close(s.checked)
	}()
	if got := profileGyro(b, ""); got != backend.GyroMouse {
		t.Errorf("read %v before the first check was done", got)
	}
	if got := profileGyro(&backend.Backend{}, ""); got != backend.GyroUnknown {
		t.Errorf("no setup: %v", got)
	}
	for _, use := range []backend.GyroUse{backend.GyroMouse, backend.GyroElsewhere, backend.GyroUnknown} {
		if text := gyroInUse(use); !strings.Contains(text, "Passthru") {
			t.Errorf("%v: %q", use, text)
		}
	}
}

// TestRestBiasFile: -gyrotest saves the drift for the motion it measured:
// the UDP server's, else the virtual DualSense's.
func TestRestBiasFile(t *testing.T) {
	b := backend.NewDS4Windows(backend.Parts{})
	if got := restBiasFile(b); got != backend.DS4WindowsBiasFile {
		t.Errorf("no motion state: %s", got)
	}
	for src, want := range map[backend.MotionSource]string{
		backend.SourceNone: backend.DS4WindowsBiasFile,
		backend.SourcePad:  backend.DS4WindowsBiasFile,
		backend.SourceUDP:  backend.DS4WindowsUDPBiasFile,
	} {
		b.MotionState = func() backend.MotionState { return backend.MotionState{Source: src} }
		if got := restBiasFile(b); got != want {
			t.Errorf("%v: %s, want %s", src, got, want)
		}
	}
	if got := restBiasFile(&backend.Backend{BiasFile: "gyro_calibration.json"}); got != "gyro_calibration.json" {
		t.Errorf("DSX: %s", got)
	}
}
