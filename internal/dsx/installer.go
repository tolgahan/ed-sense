package dsx

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Installer states, as State gives them.
const (
	StateNoFolder    = "no_folder"     // DSX's folder was not found
	StateMissing     = "missing"       // DSX has no "Elite Dangerous" profile
	StatePresent     = "present"       // it has one, and DSX uses it when Elite starts
	StateNotForElite = "not_for_elite" // it has one, but DSX's game profile for Elite names another or none
	StateWaiting     = "waiting"       // a write waits for DSX to be closed
	StateWriting     = "writing"       // the write runs, and can no longer be cancelled
	StateDone        = "done"          // written; DSX uses it once it starts again
	StateFailed      = "failed"        // not written
)

// Job states.
const (
	JobWaiting = "waiting" // for DSX to be closed
	JobDone    = "done"    // written; shown until DSX runs again
	JobFailed  = "failed"  // not written; shown until the next request
)

// Job is the write asked for last.
type Job struct {
	State   string // "" when there is none
	Reset   bool   // the player's "Elite Dangerous" profile is replaced
	Auto    bool   // the first add, which EDSense asked for itself
	Folder  string // DSX's folder when it was asked for: the write goes there
	Writing bool   // JobWaiting: the write runs now
	Err     string // JobFailed: why, without the folders of its paths
}

// Look is what DSX's files hold now.
type Look struct {
	Folder   string // DSX's folder; "" when it was not found
	Running  bool
	Has      bool   // the "Elite Dangerous" profile is there
	ForElite string // the profile DSX's game profile for Elite names; "" when none
}

// State is what the Installer is at: its job's state while it has one,
// else what DSX's files hold.
type State struct {
	State string
	Msg   string // StateFailed: why; StateNotForElite: the profile Elite gets, "" when none
	Look  Look
	Job   Job
}

// StateOf is the state of the job j over the files l. A failed add is
// shown while the profile is still missing; a failed reset until the next
// request.
func StateOf(l Look, j Job) State {
	s := State{Look: l, Job: j}
	switch {
	case j.State == JobWaiting && j.Writing:
		s.State = StateWriting
	case j.State == JobWaiting:
		s.State = StateWaiting
	case j.State == JobDone:
		s.State = StateDone
	case j.State == JobFailed && (j.Reset || !l.Has):
		s.State, s.Msg = StateFailed, j.Err
	case l.Folder == "":
		s.State = StateNoFolder
	case !l.Has:
		s.State = StateMissing
	case l.ForElite != ProfileName:
		s.State, s.Msg = StateNotForElite, l.ForElite
	default:
		s.State = StatePresent
	}
	return s
}

var (
	// ErrNoFolder: DSX's folder was not found, so nothing can be written.
	ErrNoFolder = errors.New("DSX's folder was not found")
	// ErrNothing: DSX has the profile already, and no reset was asked for.
	ErrNothing = errors.New("DSX has an \"Elite Dangerous\" profile already")
	// ErrBusy: the profile is being written.
	ErrBusy = errors.New("EDSense is writing DSX's profile")
)

// InstallerOptions set up an Installer. Nil functions look at the system.
type InstallerOptions struct {
	Backups string                    // the folder the copies go in: dsx_profile_backups next to EDSense
	Notify  func(msg string)          // tells the player; nil: nobody
	Running func() bool               // DSX runs
	Folder  func(running bool) string // DSX's folder: the running DSX's, else one in a Steam library
	Elite   func() string             // Elite's exe, for a new game profile
	Now     func() time.Time
}

// Installer adds EDSense's DSX profile for Elite, and puts it back on
// request, off the loop. DSX keeps its profiles in memory and saves them
// when it exits, so files are written only while DSX is closed. Step
// drives it, every second or so while a job waits; the other methods may
// be called from any goroutine.
type Installer struct {
	o InstallerOptions

	mu           sync.Mutex
	job          Job
	gen          int  // counts requests and cancels
	writing      bool // a write runs, without mu
	toldWait     bool // the waiting line is logged
	checked      bool // the first check found DSX's folder, or the player asked or cancelled
	toldNoFolder bool
	folder       string // DSX's folder as last found: a DSX outside the Steam libraries is found only while it runs
}

// NewInstaller makes an Installer.
func NewInstaller(o InstallerOptions) *Installer {
	if o.Running == nil {
		o.Running = dsxRunning
	}
	if o.Folder == nil {
		o.Folder = findDSXFolder
	}
	if o.Elite == nil {
		o.Elite = findEliteExe
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Installer{o: o}
}

func (i *Installer) notify(msg string) {
	if i.o.Notify != nil {
		i.o.Notify(msg)
	}
}

// find is DSX's folder: the one found now, else the one found last while
// it still holds DSX's settings.
func (i *Installer) find(running bool) string {
	found := i.o.Folder(running)
	i.mu.Lock()
	if found != "" {
		i.folder = found
	}
	last := i.folder
	i.mu.Unlock()
	if found == "" && last != "" && isDSXFolder(last) {
		found = last
	}
	return found
}

// Look reads DSX's files. A job that is done ends once DSX runs again:
// DSX then has the profile.
func (i *Installer) Look() Look {
	running := i.o.Running()
	l := Look{Running: running, Folder: i.find(running)}
	if l.Folder != "" {
		l.Has = exists(profilePath(l.Folder))
		l.ForElite = eliteProfile(l.Folder)
	}
	if running {
		i.mu.Lock()
		if i.job.State == JobDone {
			i.job = Job{}
		}
		i.mu.Unlock()
	}
	return l
}

// Job is the write asked for last.
func (i *Installer) Job() Job {
	i.mu.Lock()
	defer i.mu.Unlock()
	j := i.job
	j.Writing = i.writing && j.State == JobWaiting
	return j
}

// State looks at DSX's files and tells where the Installer is.
func (i *Installer) State() State {
	l := i.Look()
	return StateOf(l, i.Job())
}

// FirstChecked: the first add has looked, or the player asked for the
// profile or cancelled it, so FirstCheck adds nothing now.
func (i *Installer) FirstChecked() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.checked
}

// Waiting: a write waits for DSX to be closed.
func (i *Installer) Waiting() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.job.State == JobWaiting
}

// Request asks for EDSense's profile to be added, or with reset to
// replace DSX's "Elite Dangerous" profile, as soon as DSX is closed, in
// the DSX folder found now. It refuses when DSX's folder is not found,
// when there is nothing to add, and while a write runs. A request while
// one waits replaces it. The player's request ends the first add: it is
// not made after it.
func (i *Installer) Request(reset bool) error {
	running := i.o.Running()
	folder := i.find(running)
	if folder == "" {
		return ErrNoFolder
	}
	if !reset && exists(profilePath(folder)) {
		return ErrNothing
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.writing {
		return ErrBusy
	}
	i.gen++
	i.job = Job{State: JobWaiting, Reset: reset, Folder: folder}
	i.toldWait, i.checked = false, true
	return nil
}

// Cancel ends a write that waits; false when none waits. The first add
// does not make a write that was cancelled: Request and FirstCheck ended
// it before.
func (i *Installer) Cancel() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.job.State != JobWaiting || i.writing {
		return false
	}
	i.gen++
	i.job = Job{}
	return true
}

// FirstCheck is the first add: when DSX has no "Elite Dangerous" profile,
// EDSense's is added as soon as DSX is closed. It reports whether the
// check is done; until DSX's folder is found it is not, and it may be
// tried again.
func (i *Installer) FirstCheck() bool {
	running := i.o.Running()
	folder := i.find(running)
	i.mu.Lock()
	switch {
	case i.checked:
		i.mu.Unlock()
		return true
	case folder == "":
		if !i.toldNoFolder {
			log.Print("DSX folder not found: EDSense's Elite controller profile was not added (the Controller page tries again)")
			i.toldNoFolder = true
		}
		i.mu.Unlock()
		return false
	}
	i.checked = true
	if exists(profilePath(folder)) || i.job.State == JobWaiting || i.writing {
		i.mu.Unlock()
		return true
	}
	i.gen++
	i.job = Job{State: JobWaiting, Auto: true, Folder: folder}
	i.toldWait = running
	i.mu.Unlock()
	log.Printf("DSX has no %q profile yet: EDSense adds its own when DSX is closed", ProfileName)
	if running {
		i.notify("EDSense comes with a DSX controller profile for Elite Dangerous (gyro aim, touchpad and trigger setup).\n\n" +
			"It will be added the next time DSX is closed (DSX tray icon > Exit). Then start DSX again.")
	}
	return true
}

// Step writes the profile when a job waits and DSX is closed.
func (i *Installer) Step() {
	i.mu.Lock()
	if i.job.State != JobWaiting || i.writing {
		i.mu.Unlock()
		return
	}
	gen, job := i.gen, i.job
	i.mu.Unlock()

	running := i.o.Running()
	i.mu.Lock()
	if gen != i.gen {
		i.mu.Unlock()
		return
	}
	if running {
		if !i.toldWait {
			log.Print("DSX profile: waiting for DSX to be closed")
			i.toldWait = true
		}
		i.mu.Unlock()
		return
	}
	i.writing = true
	i.mu.Unlock()

	// the folder the request was made for, as long as it holds DSX's
	// settings
	folder := job.Folder
	if folder == "" || !isDSXFolder(folder) {
		folder = i.find(false)
	}
	var did string
	err := ErrNoFolder
	if folder != "" {
		did, err = i.write(folder, job.Reset)
	}

	i.mu.Lock()
	i.writing, i.toldWait = false, false
	switch {
	case err != nil:
		i.job = Job{State: JobFailed, Reset: job.Reset, Auto: job.Auto, Folder: folder, Err: plainErr(err)}
	case did != "":
		i.job = Job{State: JobDone, Reset: job.Reset, Auto: job.Auto, Folder: folder}
	default: // the profile is there already, and no reset was asked for
		i.job = Job{}
	}
	i.mu.Unlock()

	switch {
	case folder == "":
		i.notify("DSX's folder was not found, so the profile could not be written.")
	case err != nil:
		log.Printf("DSX profile not written: %v", err)
		i.notify("The DSX profile could not be written:\n" + err.Error())
	case did != "":
		log.Printf("DSX profile %q: %s", ProfileName, did)
		i.notify(fmt.Sprintf("The %q controller profile is now in DSX. Start DSX again to use it.", ProfileName))
	}
}

// write writes the profile into folder, with a panic made an error: the
// job fails, and the Installer and EDSense go on.
func (i *Installer) write(folder string, reset bool) (did string, err error) {
	defer func() {
		if r := recover(); r != nil {
			did, err = "", fmt.Errorf("EDSense hit a bug while writing: %v", r)
		}
	}()
	return installProfile(folder, i.o.Backups, i.o.Elite(), reset, i.o.Now())
}

// plainErr is an error's text with the files it names cut to their
// names, for the window.
func plainErr(err error) string {
	msg := err.Error()
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Path != "" {
		msg = strings.ReplaceAll(msg, pe.Path, filepath.Base(pe.Path))
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		for _, p := range []string{le.Old, le.New} {
			if p != "" {
				msg = strings.ReplaceAll(msg, p, filepath.Base(p))
			}
		}
	}
	return msg
}
