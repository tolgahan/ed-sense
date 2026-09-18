package elite

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Event is one journal entry.
type Event map[string]any

func (e Event) Name() string { return e.Text("event") }

func (e Event) Text(key string) string {
	v, _ := e[key].(string)
	return v
}

func (e Event) Number(key string) (float64, bool) {
	v, ok := e[key].(float64)
	return v, ok
}

func (e Event) Bool(key string) bool {
	v, _ := e[key].(bool)
	return v
}

// JournalTailer follows the newest Journal.*.log in a folder.
type JournalTailer struct {
	dir       string
	file      string
	f         *os.File
	offset    int64
	partial   []byte
	lastCheck time.Time
}

func NewJournalTailer(dir string) *JournalTailer { return &JournalTailer{dir: dir} }

// Poll calls handle for every complete new journal line. live is false while
// catching up on the file that was already there at start: state is rebuilt
// from it; its events are not played.
func (j *JournalTailer) Poll(handle func(ev Event, live bool)) {
	first := j.file == ""
	if first || time.Since(j.lastCheck) > 2*time.Second {
		j.lastCheck = time.Now()
		if newest := NewestJournal(j.dir); newest != "" && newest != j.file {
			j.open(newest, first, handle)
			return
		}
	}
	if j.f != nil {
		j.drain(handle, true)
	}
}

func (j *JournalTailer) open(path string, first bool, handle func(Event, bool)) {
	if j.f != nil {
		j.drain(handle, true) // finish the old file first
		_ = j.f.Close()
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	if !first {
		log.Printf("Journal: %s", filepath.Base(path))
	}
	j.f, j.file, j.offset, j.partial = f, path, 0, nil
	j.drain(handle, !first)
}

func (j *JournalTailer) drain(handle func(Event, bool), live bool) {
	if _, err := j.f.Seek(j.offset, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(bufio.NewReader(j.f))
	if err != nil || len(data) == 0 {
		return
	}
	j.offset += int64(len(data))
	lines := bytes.Split(append(j.partial, data...), []byte{'\n'})
	j.partial = append([]byte(nil), lines[len(lines)-1]...)
	for _, line := range lines[:len(lines)-1] {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev Event
		if json.Unmarshal(line, &ev) == nil {
			handle(ev, live)
		}
	}
}

// NewestJournal is the most recently written Journal.*.log in dir, or "".
func NewestJournal(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "Journal.*.log"))
	type journal struct {
		path string
		mod  time.Time
	}
	var list []journal
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil {
			list = append(list, journal{m, st.ModTime()})
		}
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, k int) bool {
		if list[i].mod.Equal(list[k].mod) {
			return list[i].path < list[k].path
		}
		return list[i].mod.Before(list[k].mod)
	})
	return list[len(list)-1].path
}
