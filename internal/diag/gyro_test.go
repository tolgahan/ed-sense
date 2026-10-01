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
