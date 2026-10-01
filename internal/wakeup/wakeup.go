// Package wakeup wakes whoever waits on it, without ever waiting itself.
package wakeup

import (
	"sync"
	"sync/atomic"
)

// Group is a set of 1-slot channels. Wake never blocks: a channel that
// still holds a wake is left as it is.
type Group struct {
	mu   sync.Mutex // for Add and its stop only
	subs atomic.Pointer[[]chan struct{}]
}

// Add returns a channel woken on each Wake, until stop is called. stop may
// be called more than once.
func (w *Group) Add() (wake <-chan struct{}, stop func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	var list []chan struct{}
	if p := w.subs.Load(); p != nil {
		list = append(list, *p...)
	}
	list = append(list, ch)
	w.subs.Store(&list)
	w.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			var rest []chan struct{}
			if p := w.subs.Load(); p != nil {
				for _, c := range *p {
					if c != ch {
						rest = append(rest, c)
					}
				}
			}
			w.subs.Store(&rest)
		})
	}
}

// Wake wakes every channel added and not stopped.
func (w *Group) Wake() {
	p := w.subs.Load()
	if p == nil {
		return
	}
	for _, ch := range *p {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Any reports whether someone waits.
func (w *Group) Any() bool {
	p := w.subs.Load()
	return p != nil && len(*p) > 0
}
