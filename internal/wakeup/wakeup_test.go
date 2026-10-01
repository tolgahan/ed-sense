package wakeup

import (
	"sync"
	"testing"
)

func TestWake(t *testing.T) {
	var w Group
	if w.Any() {
		t.Fatal("watched before anyone was added")
	}
	w.Wake() // nobody: nothing to do
	a, stopA := w.Add()
	b, stopB := w.Add()
	w.Wake()
	w.Wake() // the slot is full: never blocks
	for name, ch := range map[string]<-chan struct{}{"a": a, "b": b} {
		select {
		case <-ch:
		default:
			t.Errorf("%s not woken", name)
		}
		select {
		case <-ch:
			t.Errorf("%s woken twice for one slot", name)
		default:
		}
	}
	stopA()
	stopA() // twice is fine
	w.Wake()
	select {
	case <-a:
		t.Error("woken after stop")
	default:
	}
	select {
	case <-b:
	default:
		t.Error("b not woken")
	}
	if !w.Any() {
		t.Error("b is still waiting")
	}
	stopB()
	if w.Any() {
		t.Error("watched with nobody left")
	}
}

// TestWakeWhileAdding: Wake, Add and stop from many goroutines at once.
func TestWakeWhileAdding(t *testing.T) {
	var w Group
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_, stop := w.Add()
				w.Wake()
				stop()
			}
		}()
	}
	wg.Wait()
	if w.Any() {
		t.Error("a channel was left behind")
	}
}
