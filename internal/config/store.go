package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tolgahan/ed-sense/internal/wakeup"
)

// Snapshot is what the settings file holds, as the Store last saw it.
// Nobody changes it: Config is a copy of its own.
type Snapshot struct {
	Rev      int64      // per process, from 1
	Config   Config     // normalised; the last good one while the file does not parse (Default() at start)
	Problem  *FileError // the file does not parse
	FirstRun bool       // Problem == nil && Config.Backend == ""
	From     string     // who made it: "file" when read from the file, else what Set or Patch was told
}

// PatchResult is what Patch did.
type PatchResult struct {
	Applied  bool       // written, or there was nothing to change
	Rev      int64      // the Snapshot's rev after the patch
	Changed  []string   // the settings and map entries that changed, in the Schema's order
	Problems []Problem  // why the patch was refused
	Broken   *FileError // the file does not parse, so nothing was written
}

// ErrRetry is Reload's answer while a change is being written: nothing was
// read, and Recheck reports true until the next Reload.
var ErrRetry = errors.New("the settings file is being written")

// UnreadError is Reload's error when the file could not be read at all
// (another program holds it, say). Nothing was published, and the file
// should be read again later.
type UnreadError struct{ Err error }

func (e *UnreadError) Error() string { return e.Err.Error() }
func (e *UnreadError) Unwrap() error { return e.Err }

// Store is the one writer of the settings file in a running EDSense. The
// tray and the window change settings through it, and the loop reads the
// file back through Reload. It publishes what the file holds as a Snapshot.
type Store struct {
	path    string
	kick    chan struct{}
	w       wakeup.Group
	snap    atomic.Pointer[Snapshot]
	recheck atomic.Bool

	mu   sync.Mutex // held while the file is read for a change and written, and while Reload publishes
	rev  int64
	hash [sha256.Size]byte // of the bytes the Snapshot came from
	own  ownWrite
}

// ownWrite is the Store's last write, which Reload tells from an editor's
// save.
type ownWrite struct {
	hash [sha256.Size]byte // zero: none, or the write carried an edit made elsewhere
	read bool              // Reload has read it
	mod  time.Time         // the file's time when Reload first read it
}

// OpenStore reads the settings file as LoadInfo does: a missing file is
// created and an older one rewritten. It logs nothing. The Store is there
// even with an error, holding the defaults when the file does not parse.
func OpenStore(path string) (*Store, Info, error) {
	s := &Store{path: path, kick: make(chan struct{}, 1)}
	raw, cfg, info, err := readFile(path)
	h := sha256.Sum256(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil && (info.Origin == Created || info.Origin == Migrated) {
		var b []byte
		if b, err = encode(cfg); err == nil {
			if err = s.writeOwn(b, true); err == nil {
				h = sha256.Sum256(b)
				// the loop starts from this Snapshot: as good as read
				if st, serr := os.Stat(path); serr == nil {
					s.own.read, s.own.mod = true, st.ModTime()
				}
			}
		}
	}
	s.publish(cfg, info.Problem, h, "file")
	return s, info, err
}

// Path is the settings file.
func (s *Store) Path() string { return s.path }

// Snapshot is the latest one published. It never waits.
func (s *Store) Snapshot() *Snapshot { return s.snap.Load() }

// Watch wakes the returned channel on each new Snapshot, until stop is
// called.
func (s *Store) Watch() (wake <-chan struct{}, stop func()) { return s.w.Add() }

// Kick holds a wake when Set or Patch wrote the file: the loop applies the
// change at once.
func (s *Store) Kick() <-chan struct{} { return s.kick }

// Recheck reports, once, that Reload was answered with ErrRetry and the
// file should be read again.
func (s *Store) Recheck() bool { return s.recheck.Swap(false) }

// Set changes settings in the file: the tray's toggles and the controller
// app choice. from names the writer in the Snapshot. A file that does not
// parse is not written, and its error is returned; a missing one starts
// from the defaults.
func (s *Store) Set(change func(*Config), from string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, raw, _, err := s.read()
	if err != nil {
		return err
	}
	change(&cfg)
	cfg.normalise()
	return s.write(cfg, raw, from)
}

// Patch merges a settings patch from the window into the file (see
// MergePatch). A patch with problems, one that sets a folder that is not
// there, or one on a file that does not parse, is refused without an
// error and nothing is written; the error is a failed write.
func (s *Store) Patch(patch []byte, from string) (PatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, raw, fe, err := s.read()
	if fe != nil {
		return PatchResult{Rev: s.rev, Problems: []Problem{{Code: CodeBroken, Msg: fe.Msg}}, Broken: fe}, nil
	}
	if err != nil {
		return PatchResult{Rev: s.rev}, err
	}
	next, changed, problems := MergePatch(cur, patch)
	if len(problems) == 0 {
		problems = missingFolders(next, changed)
	}
	if len(problems) > 0 {
		return PatchResult{Rev: s.rev, Problems: problems}, nil
	}
	if len(changed) == 0 {
		return PatchResult{Applied: true, Rev: s.rev}, nil
	}
	if err := s.write(next, raw, from); err != nil {
		return PatchResult{Rev: s.rev}, err
	}
	log.Printf("Settings changed in the %s: %s", from, strings.Join(changed, ", "))
	return PatchResult{Applied: true, Rev: s.rev, Changed: changed}, nil
}

// missingFolders lists the folder settings in changed that name a folder
// that is not there. "" is Elite's standard folder, so it is not looked
// at. The message never holds the path.
func missingFolders(next Config, changed []string) []Problem {
	index := schemaIndex()
	var doc map[string]any
	var out []Problem
	for _, name := range changed {
		if index[name].Type != typePath {
			continue
		}
		if doc == nil {
			doc, _ = configDoc(next)
		}
		dir, _ := doc[name].(string)
		if dir == "" {
			continue
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			out = append(out, Problem{Path: name, Code: CodeMissing, Msg: "no such folder"})
		}
	}
	return out
}

// Reload reads the file for the loop and never writes it: a file from an
// older version is migrated in memory, and the next Set or Patch writes
// it. own is true when the bytes are the Store's last write, read for the
// first time or again with the time they had then: an editor's save, even
// of the same bytes, is not. A new Snapshot is published when the bytes
// differ from the last one's; a file that does not parse publishes its
// Problem with the last good settings, and its error is returned. A file
// that could not be read publishes nothing and gives an *UnreadError.
// Reload never waits for a write under way: it returns ErrRetry, and
// Recheck asks for another read.
func (s *Store) Reload() (cfg Config, own bool, err error) {
	if !s.mu.TryLock() {
		s.recheck.Store(true)
		return Config{}, false, ErrRetry
	}
	defer s.mu.Unlock()
	raw, cfg, info, err := readFile(s.path)
	if info.Origin == Created {
		return Config{}, false, fmt.Errorf("%s: %w", filepath.Base(s.path), fs.ErrNotExist)
	}
	if raw == nil && err != nil {
		return Config{}, false, &UnreadError{err}
	}
	var mod time.Time
	if st, serr := os.Stat(s.path); serr == nil {
		mod = st.ModTime()
	}
	h := sha256.Sum256(raw)
	own = s.isOwn(h, mod)
	if h != s.hash {
		if err != nil {
			s.publish(s.snap.Load().Config, info.Problem, h, "file")
		} else {
			s.publish(cfg.Clone(), nil, h, "file")
		}
	}
	if err != nil {
		return Config{}, own, err
	}
	return cfg, own, nil
}

// read reads the file for a change, under s.mu: a missing file gives the
// defaults, and one that cannot be used its Problem and error.
func (s *Store) read() (Config, []byte, *FileError, error) {
	raw, cfg, info, err := readFile(s.path)
	return cfg, raw, info.Problem, err
}

// write saves cfg when its text differs from raw, the file as read, and
// publishes it. Under s.mu.
func (s *Store) write(cfg Config, raw []byte, from string) error {
	b, err := encode(cfg)
	if err != nil {
		return err
	}
	if bytes.Equal(b, raw) {
		return nil
	}
	// the file as read holds what the Snapshot does, unless an editor
	// changed it since: then the write carries that edit too
	if err := s.writeOwn(b, sha256.Sum256(raw) == s.hash); err != nil {
		return err
	}
	s.publish(cfg.Clone(), nil, sha256.Sum256(b), from) // Set's change may keep a map it put in
	select {
	case s.kick <- struct{}{}:
	default:
	}
	return nil
}

// writeOwn writes b as the file. Reload reads it as the Store's own when
// mine (b holds only changes made through the Store), else as an editor's
// save; a failed write is forgotten. Under s.mu.
func (s *Store) writeOwn(b []byte, mine bool) error {
	s.own = ownWrite{}
	if mine {
		s.own.hash = sha256.Sum256(b)
	}
	if err := writeReplacing(s.path, b); err != nil {
		s.own = ownWrite{}
		return err
	}
	return nil
}

// isOwn: the file, with hash h and time mod, is the Store's last write,
// read for the first time or again with the time it had then. Anything
// else is an editor's save, after which the Store's write is gone. Under
// s.mu.
func (s *Store) isOwn(h [sha256.Size]byte, mod time.Time) bool {
	switch {
	case s.own.hash == [sha256.Size]byte{} || h != s.own.hash:
	case !s.own.read:
		s.own.read, s.own.mod = true, mod
		return true
	case mod.Equal(s.own.mod):
		return true
	}
	s.own = ownWrite{}
	return false
}

// Check reads the file as Set and Patch do, and writes nothing: the
// problem a change would meet now (with its error), or nil. It sees an
// edit the loop has not read yet.
func (s *Store) Check() (*FileError, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, fe, err := s.read()
	return fe, err
}

// publish makes cfg the Snapshot and wakes the watchers. Under s.mu.
func (s *Store) publish(cfg Config, problem *FileError, h [sha256.Size]byte, from string) {
	s.rev++
	s.hash = h
	s.snap.Store(&Snapshot{Rev: s.rev, Config: cfg, Problem: problem, FirstRun: problem == nil && cfg.Backend == "", From: from})
	s.w.Wake()
}
