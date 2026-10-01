package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/elite"
)

// count is how many lines of the transcript so far contain s.
func (p *player) count(s string) int {
	p.flush()
	n := 0
	for _, l := range p.out {
		if strings.Contains(l, s) {
			n++
		}
	}
	return n
}

func (p *player) fileTime() time.Time {
	st, err := os.Stat(p.s.store.Path())
	if err != nil {
		p.t.Fatal(err)
	}
	return st.ModTime()
}

// TestCheckConfigOwnWrites: EDSense's own writes apply without "Settings
// reloaded", at the 2 s check or at once when forced; a forced check takes
// the file's time, so the 2 s check does not read the file again.
func TestCheckConfigOwnWrites(t *testing.T) {
	p := newPlayer(t, nil)
	p.dsx.Answering = true
	p.writeStatus(elite.Status{Flags: ship})
	p.run(time.Second)
	if err := p.s.store.Set(func(c *config.Config) { c.GyroAim = false }, "tray"); err != nil {
		t.Fatal(err)
	}
	setMod(t, p.s.store.Path(), p.now)
	p.run(3 * time.Second)
	if p.s.cfg.GyroAim {
		t.Fatal("the 2 s check did not apply the Store's write")
	}

	if err := p.s.store.Set(func(c *config.Config) { c.GyroAim = true }, "tray"); err != nil {
		t.Fatal(err)
	}
	p.s.checkConfig(p.now, true)
	if !p.s.cfg.GyroAim {
		t.Fatal("the forced check did not apply the Store's write")
	}
	written := p.fileTime()
	if !p.s.cfgMod.Equal(written) {
		t.Errorf("the forced check left cfgMod at %v, the file is %v", p.s.cfgMod, written)
	}
	if n := p.count("Settings reloaded"); n != 0 {
		t.Errorf("own writes said Settings reloaded %d times", n)
	}

	// an edit with the time the forced check took is not seen; a later one
	// is (its bytes differ from the Store's writes)
	p.config(func(c *config.Config) { c.GyroAim, c.HapticsStrength = false, 0.5 })
	setMod(t, p.s.store.Path(), written)
	p.run(3 * time.Second)
	if !p.s.cfg.GyroAim {
		t.Error("an edit with the same time was read")
	}
	setMod(t, p.s.store.Path(), written.Add(time.Second))
	p.run(3 * time.Second)
	if p.s.cfg.GyroAim || p.count("Settings reloaded") != 1 {
		t.Errorf("the edit was not read once: gyro_aim %v\n%s", p.s.cfg.GyroAim, strings.Join(p.out, "\n"))
	}
}

// TestCheckConfigBroken: a file that does not parse keeps the settings and
// shows in the Snapshot; the fixed file is read again.
func TestCheckConfigBroken(t *testing.T) {
	p := newPlayer(t, nil)
	p.run(time.Second)
	path := p.s.store.Path()
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"gyro_aim": false,}`), 0o644); err != nil {
		t.Fatal(err)
	}
	setMod(t, path, p.now)
	p.run(3 * time.Second)
	if p.count("log Settings not reloaded: edsense.json: ") != 1 || !p.s.cfg.GyroAim {
		t.Fatalf("broken file: gyro_aim %v\n%s", p.s.cfg.GyroAim, strings.Join(p.out, "\n"))
	}
	if snap := p.s.store.Snapshot(); snap.Problem == nil || !snap.Config.GyroAim {
		t.Errorf("snapshot %+v", *snap)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(good), `"gyro_aim": true`, `"gyro_aim": false`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	setMod(t, path, p.now)
	p.run(3 * time.Second)
	if p.count("log Settings reloaded") != 1 || p.s.cfg.GyroAim {
		t.Errorf("fixed file: gyro_aim %v", p.s.cfg.GyroAim)
	}
	if snap := p.s.store.Snapshot(); snap.Problem != nil || snap.Config.GyroAim {
		t.Errorf("snapshot %+v", *snap)
	}
}

// TestCheckConfigUnreadable: a file that could not be read (another
// program holds it, say) shows no problem in the Snapshot, is said once,
// and is read again at the next 2 s check, though its time has not
// changed since.
func TestCheckConfigUnreadable(t *testing.T) {
	p := newPlayer(t, nil)
	p.run(time.Second)
	path := p.s.store.Path()
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	at := p.now
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil { // a folder cannot be read as the file
		t.Fatal(err)
	}
	setMod(t, path, at)
	p.run(5 * time.Second)
	if n := p.count("log Settings not reloaded: "); n != 1 {
		t.Errorf("said %d times:\n%s", n, strings.Join(p.out, "\n"))
	}
	if snap := p.s.store.Snapshot(); snap.Problem != nil || snap.Rev != 1 {
		t.Errorf("snapshot %+v", *snap)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(good), `"gyro_aim": true`, `"gyro_aim": false`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	setMod(t, path, at)
	p.run(3 * time.Second)
	if p.s.cfg.GyroAim || p.count("log Settings reloaded") != 1 {
		t.Errorf("not read again: gyro_aim %v\n%s", p.s.cfg.GyroAim, strings.Join(p.out, "\n"))
	}
}

// TestCheckConfigWhileWriting: a check that meets a write under way says
// nothing and reads again at the next tick; the last writer wins.
func TestCheckConfigWhileWriting(t *testing.T) {
	p := newPlayer(t, nil)
	p.run(time.Second)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error)
	go func() {
		done <- p.s.store.Set(func(c *config.Config) {
			close(entered)
			<-release
			c.PollMs = 40
		}, "tray")
	}()
	<-entered
	p.config(func(c *config.Config) { c.GyroAim = false })
	p.run(2 * time.Second)
	if !p.s.cfg.GyroAim || p.count("Settings") != 0 {
		t.Fatalf("read while the Store wrote:\n%s", strings.Join(p.out, "\n"))
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p.run(time.Duration(p.s.cfg.PollMs) * time.Millisecond) // one tick
	if p.s.cfg.PollMs != 40 || !p.s.cfg.GyroAim {
		t.Errorf("not read again at the next tick: poll_ms %d, gyro_aim %v", p.s.cfg.PollMs, p.s.cfg.GyroAim)
	}
	if p.count("Settings") != 0 {
		t.Errorf("logged:\n%s", strings.Join(p.out, "\n"))
	}
}
