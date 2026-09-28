package bindings

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Watcher reloads the bindings when the player changes preset or edits it.
type Watcher struct {
	dir      string
	file     string
	fileMod  time.Time
	startMod time.Time
	checked  time.Time
	current  *Bindings
}

func NewWatcher(dir string) *Watcher { return &Watcher{dir: dir} }

// Bindings are the current bindings (nil without a custom preset).
func (w *Watcher) Bindings() *Bindings { return w.current }

// Poll checks for changes every 5 s and reports whether the bindings changed.
func (w *Watcher) Poll(now time.Time) bool {
	if w.dir == "" || now.Sub(w.checked) < 5*time.Second {
		return false
	}
	w.checked = now
	var startMod time.Time
	for _, n := range presetStartFiles {
		if st, err := os.Stat(filepath.Join(w.dir, n)); err == nil {
			startMod = st.ModTime()
			break
		}
	}
	preset, file := ShipPreset(w.dir)
	var fileMod time.Time
	if file != "" {
		if st, err := os.Stat(file); err == nil {
			fileMod = st.ModTime()
		}
	}
	if file == w.file && fileMod.Equal(w.fileMod) && startMod.Equal(w.startMod) && (w.current != nil || file == "") {
		return false
	}
	w.file, w.fileMod, w.startMod = file, fileMod, startMod
	if file == "" {
		if preset != "" {
			log.Printf("Bindings: %q is a built-in preset; heat sink, chaff and shield cell feel need a custom preset", preset)
		}
		w.current = nil
		return true
	}
	data, err := os.ReadFile(file)
	if err != nil {
		log.Printf("Bindings: %v", err)
		return false
	}
	b, err := Parse(data)
	if err != nil {
		log.Printf("Bindings: %s: %v", filepath.Base(file), err)
		return false
	}
	b.File, w.current = file, b
	var bound []string
	for _, a := range watched {
		if b.Has(a) {
			bound = append(bound, string(a))
		}
	}
	log.Printf("Bindings: %s (%s; %s)", filepath.Base(file), strings.Join(bound, ", "), b.Mouse)
	return true
}
