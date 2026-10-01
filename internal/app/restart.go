package app

import (
	"errors"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// restartReq asks the loop to end the session and start the next one.
type restartReq struct {
	b        *backend.Backend // nil: the same backend, a new session
	idleOnly bool             // refused while EDSense drives the controller
	adopt    bool             // the next session keeps the game state
	keys     *Keys            // what the next session is built from; nil: the loop's settings
	done     chan error
}

// Keys are the settings a session is built from, which change only with
// the next session.
type Keys struct {
	JournalDir, BindingsDir string
	PollMs                  int
}

// KeysOf are cfg's session keys.
func KeysOf(cfg *config.Config) Keys {
	return Keys{JournalDir: cfg.JournalDir, BindingsDir: cfg.BindingsDir, PollMs: cfg.PollMs}
}

var (
	// ErrBusy: the gyro calibrates, or (idleOnly) EDSense drives the
	// controller or a demo plays. Nothing changed.
	ErrBusy = errors.New("busy")
	// ErrStopped: Run has ended, or has not started. Nothing changed.
	ErrStopped = errors.New("stopped")
)

// Restart has the loop end the session and start the next one: on b when
// it is not nil (the backend attached then closes), else on the same
// backend. adopt keeps the game as the session knows it, for when
// journal_dir and bindings_dir did not change. keys are what the next
// session is built from (the engine's applied settings, so a change that
// waits in the file keeps waiting); nil takes the loop's settings.
// idleOnly refuses while EDSense drives the controller or a demo plays;
// without it a demo is cut short. A calibration refuses either. Restart
// returns once the next session is built. On an error b was not attached,
// and the caller closes it.
func (a *App) Restart(b *backend.Backend, idleOnly, adopt bool, keys *Keys) error {
	if !a.started.Load() {
		return ErrStopped
	}
	r := restartReq{b: b, idleOnly: idleOnly, adopt: adopt, keys: keys, done: make(chan error, 1)}
	again := time.NewTicker(100 * time.Millisecond)
	defer again.Stop()
	for {
		// a demo keeps the loop until it ends
		if a.demoPlaying.Load() != nil {
			if idleOnly {
				return ErrBusy
			}
			a.StopDemo()
		}
		select {
		case a.restarts <- r:
			select {
			case err := <-r.done:
				return err
			case <-a.done: // the loop died taking it
				select {
				case err := <-r.done:
					return err
				default:
					return ErrStopped
				}
			}
		case <-a.done:
			return ErrStopped
		case <-again.C:
		}
	}
}

// WakeStatus has the next tick publish the status to its watchers, even
// when it did not change.
func (a *App) WakeStatus() { a.wakeLoop() }

// Done is closed once Run has ended, after the controller was handed back
// and the audio and the pad were closed. It stays open when Run never
// started.
func (a *App) Done() <-chan struct{} { return a.done }

// takeRestart answers r on the loop and returns the session to go on with:
// s when nothing changed, the next session, or nil when stop is closed
// (quitting goes first).
func (s *session) takeRestart(r restartReq, stop <-chan struct{}) *session {
	select {
	case <-stop:
		r.done <- ErrStopped
		s.stop()
		s.close(false)
		return nil
	default:
	}
	next, err := s.restart(r, time.Now())
	r.done <- err
	return next
}

// restart ends the session for r and builds the next one. It runs on the
// loop; the golden tests call it as takeRestart does. The controller is
// handed back first, and the gyro's drift goes to the old backend's file.
func (s *session) restart(r restartReq, now time.Time) (*session, error) {
	if s.gyroStatus().Calibrating || r.idleOnly && s.active && s.online {
		return s, ErrBusy
	}
	// the loop's settings first take a change the Store wrote just before,
	// or one a read under way missed
	kicked := false
	select {
	case <-s.store.Kick():
		kicked = true
	default:
	}
	if recheck := s.store.Recheck(); kicked || recheck {
		s.checkConfig(now, true)
	}
	k := KeysOf(s.cfg)
	if r.keys != nil {
		k = *r.keys
	}
	s.stop()
	s.close(r.adopt)
	s.haptics.Silence() // the synth too: nothing of this session plays on
	if r.b != nil {
		s.detach()
		s.attach(r.b)
	}
	var next *session
	if r.adopt {
		next = s.adopt(now, k.PollMs)
	} else {
		next = s.newSessionWith(k)
		next.setHUDPalette()
	}
	s.wakeLoop()
	return next, nil
}

// close lets go of what the session holds: its setup's work, and the
// journal unless the next session keeps it (keepGame).
func (s *session) close(keepGame bool) {
	if c, ok := s.profile.(interface{ Close() }); ok {
		c.Close()
	}
	if !keepGame {
		s.journal.Close()
	}
}

// adopt is the next session on the backend attached now, ticking every
// pollMs. It keeps the game as this session knows it, so the journal is
// not read again; what the backend and the controller were told starts
// over, so the new backend gets its hand-back and a full frame. The
// haptics engine was silenced.
func (s *session) adopt(now time.Time, pollMs int) *session {
	return &session{
		App: s.App,

		game:             s.game,
		haptics:          s.haptics,
		lights:           s.lights,
		status:           s.status,
		journal:          s.journal,
		bindings:         s.bindings,
		detector:         s.detector,
		running:          s.running,
		context:          s.context,
		target:           s.target,
		lastHUDHit:       s.lastHUDHit,
		lastConfigCheck:  s.lastConfigCheck,
		lastProcessCheck: s.lastProcessCheck,

		profile:   s.newSetup(),
		pollMs:    pollMs,
		startedAt: now,
		hold:      gyro.HoldStart,
		saidUse:   -1,
	}
}
