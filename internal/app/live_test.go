package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/backend/backendtest"
	"github.com/tolgahan/ed-sense/internal/config"
)

// TestLiveNeverBlocks: the loop publishes to a watcher that never reads
// and to one that reads all the time, and never waits for either.
func TestLiveNeverBlocks(t *testing.T) {
	var l liveStore
	_, stopDead := l.w.add() // never read
	defer stopDead()
	busy, stopBusy := l.w.add()
	var wg sync.WaitGroup
	quit := make(chan struct{})
	wg.Add(1)
	go func() { // reads as fast as it can
		defer wg.Done()
		for {
			select {
			case <-quit:
				return
			case <-busy:
				l.get()
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200000; i++ {
			l.put(Live{Status: Status{Context: "supercruise"}, Controllers: i})
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("put blocked")
	}
	close(quit)
	wg.Wait()
	stopBusy()
	if v, ok := l.get(); !ok || v.Controllers != 199999 {
		t.Errorf("latest = %+v, %v", v, ok)
	}
	if !l.watched() {
		t.Error("a watcher left was not counted")
	}
	stopDead()
	if l.watched() {
		t.Error("watched with nobody watching")
	}
}

func TestLiveWakesOnChangeOnly(t *testing.T) {
	var l liveStore
	wake, stop := l.w.add()
	defer stop()
	l.put(Live{Controllers: 1})
	<-wake
	l.put(Live{Controllers: 1}) // the same
	select {
	case <-wake:
		t.Error("woken without a change")
	default:
	}
	l.put(Live{Controllers: 2})
	select {
	case <-wake:
	default:
		t.Error("not woken by a change")
	}
}

func TestNotices(t *testing.T) {
	var r notices
	wake, stop := r.w.add()
	defer stop()
	for i := 1; i <= 60; i++ {
		r.add(i%10 == 0, fmt.Sprintf("n%d", i))
	}
	select {
	case <-wake:
	default:
		t.Error("no wake for new notices")
	}
	all := r.since(0)
	if len(all) != NoticeCap || all[0].ID != 11 || all[len(all)-1].ID != 60 || all[0].Text != "n11" {
		t.Fatalf("kept %d, first %+v, last %+v", len(all), all[0], all[len(all)-1])
	}
	if !all[len(all)-1].Message || all[0].Message {
		t.Error("message flags lost")
	}
	late := r.since(57)
	if len(late) != 3 || late[0].ID != 58 {
		t.Errorf("since 57: %+v", late)
	}
	if got := r.since(60); len(got) != 0 {
		t.Errorf("since the last: %+v", got)
	}
}

func TestSay(t *testing.T) {
	w := backend.DSXWords()
	cases := []struct {
		s     Status
		level Level
		text  string
	}{
		{Status{Online: true, EliteRunning: true, Active: true, Context: "supercruise"}, LevelActive, "Active: supercruise"},
		{Status{Online: false, EliteRunning: true, Active: true}, LevelError, w.TrayOffline},
		{Status{Online: true, Paused: true}, LevelIdle, "Paused"},
		{Status{Online: true}, LevelIdle, "Waiting for Elite Dangerous"},
		{Status{Demo: true}, LevelActive, "Playing the demo"},
		{Status{Online: true, EliteRunning: true}, LevelIdle, "Elite running, not in a ship or on foot yet"},
	}
	for _, c := range cases {
		if level, text := Say(c.s, w); level != c.level || text != c.text {
			t.Errorf("%+v: %v %q, want %v %q", c.s, level, text, c.level, c.text)
		}
	}
}

// TestGoldenWatched plays golden scripts with the window watching: every
// transcript must stay exactly as it is without the window.
func TestGoldenWatched(t *testing.T) {
	names := []string{"outputs", "menus", "demo-requested", "gyro-own-flight", "gyro-own-calibrate", "ds4w-flight", "ds4w-gyro-mouse"}
	find := func(list []script, name string) (script, bool) {
		i := slices.IndexFunc(list, func(s script) bool { return s.name == name })
		if i < 0 {
			return script{}, false
		}
		return list[i], true
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			newP := newPlayer
			sc, ok := find(append(slices.Clone(scripts), gyroScripts...), name)
			if !ok {
				newP = newDS4WPlayer
				if sc, ok = find(ds4wScripts, name); !ok {
					t.Fatalf("no script %s", name)
				}
			}
			p := newP(t, sc.cfg)
			wake, stop := p.s.App.Watch()
			var seen []Live
			var mu sync.Mutex
			quit := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-quit:
						return
					case <-wake:
						if l, ok := p.s.App.Live(); ok {
							mu.Lock()
							seen = append(seen, l)
							mu.Unlock()
						}
					}
				}
			}()
			sc.play(p)
			p.flush()
			p.sum()
			close(quit)
			wg.Wait()
			stop()
			got := strings.Join(p.out, "\n") + "\n"
			backendtest.Compare(t, filepath.Join("testdata", "golden", sc.name+".txt"), got, false)
			mu.Lock()
			defer mu.Unlock()
			if len(seen) == 0 {
				t.Fatal("the watcher saw no status")
			}
			last := seen[len(seen)-1]
			if last.Backend == "" || last.Shield != -1 && last.Shield > 100 {
				t.Errorf("last status %+v", last)
			}
		})
	}
}

// TestDetailHaptics: what the Haptics tile says.
func TestDetailHaptics(t *testing.T) {
	p := newPlayer(t, nil)
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.run(time.Second)
	if got := p.s.detail(p.now, Status{}, false).Haptics; got != HapticsNative {
		t.Errorf("with the audio up: %q", got)
	}
	p.s.cfg.Haptics = false
	p.run(100 * time.Millisecond)
	if got := p.s.detail(p.now, Status{}, false).Haptics; got != HapticsOff {
		t.Errorf("haptics off: %q", got)
	}
	p.s.cfg.Haptics, p.s.cfg.HapticsMode = true, config.HapticsRumble
	p.run(100 * time.Millisecond)
	if got := p.s.detail(p.now, Status{}, false).Haptics; got != HapticsRumble {
		t.Errorf("rumble mode: %q", got)
	}
}
