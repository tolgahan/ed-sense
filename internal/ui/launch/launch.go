// Package launch starts EDSense's window process from the core (the tray
// process) and serves it: the window is `EDSense.exe --window`, talking
// to the core over its stdin and stdout (internal/control). The core never
// loads WebView2 itself; the window exits when it is closed, when the core
// says bye, or when its stdin ends.
package launch

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tolgahan/ed-sense/internal/control"
)

// Flag is the command-line flag that makes EDSense.exe the window.
const Flag = "--window"

// CouldNotStart is what the player is told after two failed starts in a row.
const CouldNotStart = "The EDSense window could not start. See edsense.log."

// NoRuntime asks about the missing WebView2 Runtime.
const NoRuntime = "The EDSense window needs Microsoft's WebView2 Runtime, which is not installed on this PC.\n\n" +
	"EDSense keeps working from its tray icon.\n\nOpen Microsoft's download page for the WebView2 Runtime?"

// Config is how the core starts its window.
type Config struct {
	Exe       string   // "": this exe
	Args      []string // nil: Flag
	Version   string
	StatePath string // ui_state.json
	Core      Core

	Ask     func(text string) bool // a Yes/No question; nil: no
	Warn    func(text string)      // nil: logged only
	Missing func() bool            // the WebView2 Runtime is missing; nil: never
	Logf    func(format string, args ...any)
}

// Window is the core's side of the window process: at most one runs.
type Window struct {
	cfg     Config
	mu      sync.Mutex
	cur     *child
	last    *child // the run before, while its job may still hold WebView2's processes
	failed  int    // starts in a row that ended before the page was up
	closing bool   // the core quits
	asking  atomic.Bool
	showing atomic.Bool // the page is up and can be seen: it shows EDSense's messages
}

// New prepares the window; nothing starts until Open.
func New(cfg Config) *Window {
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	if cfg.Args == nil {
		cfg.Args = []string{Flag}
	}
	return &Window{cfg: cfg}
}

// Open starts the window, or brings the running one to the front. It
// returns at once.
func (w *Window) Open() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closing {
		return
	}
	if c := w.cur; c != nil {
		if c.closing.Load() {
			go w.reopen(c)
			return
		}
		allowForeground(c.pid)
		c.conn.Send(control.Event{Ev: "focus"}, false)
		return
	}
	if w.cfg.Missing != nil && w.cfg.Missing() {
		go w.offerRuntime()
		return
	}
	c, err := w.start()
	if err != nil {
		w.cfg.Logf("Window: could not start: %v", err)
		w.warn(CouldNotStart)
		return
	}
	w.cur = c
}

// Showing reports whether the window is open where the player can see
// it, so a message goes there in place of a message box. It never waits.
func (w *Window) Showing() bool { return w.showing.Load() }

// Takes reports whether the window shows the message with notice id, so
// no box is needed. A window that is starting shows every message from
// its start on once its page is up, so Takes waits for that, at most
// for wait. Later messages it shows only while it can be seen. While
// EDSense quits, no box opens either.
func (w *Window) Takes(id int64, wait time.Duration) bool {
	w.mu.Lock()
	c, quitting := w.cur, w.closing
	w.mu.Unlock()
	if quitting {
		return true
	}
	if c == nil || id <= c.after {
		return false
	}
	timeout := time.After(wait)
	for !c.up.Load() && !c.rejected.Load() {
		select {
		case <-c.done:
			return w.quitting()
		case <-timeout:
			return w.quitting()
		case <-time.After(50 * time.Millisecond):
		}
	}
	switch {
	case !c.up.Load():
		return w.quitting()
	case id <= c.upID.Load():
		return true // its page showed it as it came up
	}
	return w.showing.Load() || w.quitting()
}

func (w *Window) quitting() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closing
}

// reopen opens the window again once the run whose window is going has
// ended, so the new one never joins its WebView2 processes.
func (w *Window) reopen(c *child) {
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		w.mu.Lock()
		gone := w.cur != c
		w.mu.Unlock()
		if gone {
			w.Open()
			return
		}
	}
}

// lastNotice is the ID of the core's newest notice; 0: none yet.
func lastNotice(core Core) int64 {
	if n := core.Notices(0); len(n) > 0 {
		return n[len(n)-1].ID
	}
	return 0
}

// Running reports whether the window process runs.
func (w *Window) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cur != nil
}

// Close says bye to the window and waits for it to exit, at most for
// timeout; then it is killed. The pipes stay open meanwhile, so the window
// can still save its placement. No window opens after Close.
func (w *Window) Close(timeout time.Duration) {
	w.mu.Lock()
	w.closing = true
	c, last := w.cur, w.last
	w.last = nil
	w.mu.Unlock()
	if last != nil {
		last.endJob()
	}
	if c == nil {
		return
	}
	c.conn.Send(control.Event{Ev: "bye"}, false)
	select {
	case <-c.done:
	case <-time.After(timeout):
		w.cfg.Logf("Window: did not close, stopping it")
		_ = c.cmd.Process.Kill()
		select {
		case <-c.done:
		case <-time.After(time.Second):
		}
	}
	c.closeJob()
}

func (w *Window) warn(text string) {
	if w.cfg.Warn != nil {
		go w.cfg.Warn(text)
	}
}

// offerRuntime: without the WebView2 Runtime, the player may open its
// download page. EDSense never downloads it.
func (w *Window) offerRuntime() {
	if !w.asking.CompareAndSwap(false, true) {
		return
	}
	defer w.asking.Store(false)
	w.cfg.Logf("Window: the WebView2 Runtime is not installed")
	if w.cfg.Ask != nil && w.cfg.Ask(NoRuntime) {
		w.cfg.Core.OpenURL(control.WebView2Download)
	}
}

// child is one run of the window process.
type child struct {
	w        *Window
	cmd      *exec.Cmd
	pid      int
	in       *os.File // its stdin, our end
	out      *os.File // its stdout
	errOut   *os.File // its stderr
	conn     *control.Conn
	job      io.Closer
	jobOnce  sync.Once
	jobTimer *time.Timer   // ends the job a while after the exit; set under w.mu
	after    int64         // the last notice before it started: its page shows the messages after it
	done     chan struct{} // closed when it has exited
	helloed  atomic.Bool
	rejected atomic.Bool  // hello was refused: it speaks another protocol, and shows no messages
	closing  atomic.Bool  // its window is going: the process ends soon
	up       atomic.Bool  // the page is up: WebView2 started
	upID     atomic.Int64 // the last notice when the page came up: it has shown them

	mu         sync.Mutex
	stopStatus chan struct{} // the status pump runs until closed; nil: none
}

// start runs the window process; w.mu is held.
func (w *Window) start() (*child, error) {
	// The last run's WebView2 processes may still be ending. The new window
	// could join them, as it uses the same folder, and then die with them
	// when their job ends: end them first.
	if p := w.last; p != nil {
		w.last = nil
		p.endJob()
	}
	exe := w.cfg.Exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	var files [6]*os.File // stdin r/w, stdout r/w, stderr r/w
	for i := 0; i < 6; i += 2 {
		r, wr, err := os.Pipe()
		if err != nil {
			for _, f := range files[:i] {
				f.Close()
			}
			return nil, err
		}
		files[i], files[i+1] = r, wr
	}
	inR, inW, outR, outW, errR, errW := files[0], files[1], files[2], files[3], files[4], files[5]
	cmd := exec.Command(exe, w.cfg.Args...)
	cmd.Dir = filepath.Dir(exe)
	cmd.Env = CleanEnv(os.Environ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	cmd.WaitDelay = 2 * time.Second
	after := lastNotice(w.cfg.Core)
	err := cmd.Start()
	// the child holds its own ends now
	inR.Close()
	outW.Close()
	errW.Close()
	if err != nil {
		inW.Close()
		outR.Close()
		errR.Close()
		return nil, err
	}
	for _, f := range []*os.File{inW, outR, errR} {
		noInherit(f)
	}
	c := &child{w: w, cmd: cmd, pid: cmd.Process.Pid, in: inW, out: outR, errOut: errR, after: after, done: make(chan struct{})}
	// before hello is answered, so before it can start WebView2
	job, err := newJob(cmd.Process.Pid)
	if err != nil {
		w.cfg.Logf("Window: no job object: %v", err)
	}
	c.job = job
	allowForeground(c.pid)
	c.conn = control.NewConn(inW, func() {
		w.cfg.Logf("Window: it stopped reading, closing it")
		_ = cmd.Process.Kill()
	})
	go c.logStderr()
	go c.read()
	go c.wait()
	return c, nil
}

// closeJob ends whatever is left of this run, WebView2's processes
// included, and returns once they are gone.
func (c *child) closeJob() {
	c.jobOnce.Do(func() {
		if c.job != nil {
			_ = c.job.Close()
		}
	})
}

// endJob ends the job now, in place of its timer.
func (c *child) endJob() {
	if c.jobTimer != nil {
		c.jobTimer.Stop()
	}
	c.closeJob()
}

// wait: the process's exit is the truth, whatever the pipes do.
func (c *child) wait() {
	err := c.cmd.Wait()
	w := c.w
	if err != nil {
		w.cfg.Logf("Window: exited: %v", err)
	} else {
		w.cfg.Logf("Window: closed")
	}
	c.stopPump()
	c.conn.Close()
	_ = c.in.Close()
	close(c.done)
	w.mu.Lock()
	if w.cur == c {
		w.cur = nil
	}
	// WebView2's own processes get a moment to end by themselves; the
	// job ends whatever is left, or the next start does.
	if !w.closing {
		w.last = c
		c.jobTimer = time.AfterFunc(5*time.Second, func() {
			c.closeJob()
			w.mu.Lock()
			if w.last == c {
				w.last = nil
			}
			w.mu.Unlock()
		})
	}
	// A start is good once the page is up. WebView2 starts after hello,
	// and when it fails the window exits with an error.
	tell := false
	switch {
	case c.up.Load():
		w.failed = 0
	case err != nil && !w.closing:
		if w.failed++; w.failed >= 2 {
			w.failed, tell = 0, true
		}
	}
	w.mu.Unlock()
	if tell {
		w.warn(CouldNotStart)
	}
}

// logStderr puts the window's own output in the log.
func (c *child) logStderr() {
	defer c.errOut.Close()
	s := bufio.NewScanner(c.errOut)
	s.Buffer(make([]byte, 0, 4096), control.MaxLine)
	n := 0
	for s.Scan() {
		if n++; n <= maxLogLines {
			c.w.cfg.Logf("window: %s", Clean(s.Text()))
		} else if n == maxLogLines+1 {
			c.w.cfg.Logf("window: more output, not logged")
		}
	}
	_, _ = io.Copy(io.Discard, c.errOut) // a line too long: keep the pipe flowing
}

// maxLogLines is how many lines of the window's output go in the log per
// run.
const maxLogLines = 500

// Clean makes one line of the window's output safe for the log: no
// control characters, at most 400 bytes.
func Clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > 400 {
		s = s[:400] + "..."
	}
	return s
}

func (c *child) read() {
	defer c.out.Close()
	if err := control.Read(c.out, c.handle); err != nil && !errors.Is(err, os.ErrClosed) {
		c.w.cfg.Logf("Window: %v", err)
		_ = c.cmd.Process.Kill()
	}
}

func (c *child) reply(id int64, v any) { c.conn.Send(control.Reply{ID: id, OK: v}, false) }

func (c *child) fail(id int64, e *control.Error) {
	c.conn.Send(control.Reply{ID: id, Err: e}, false)
}

// handle answers one request. The window checked it against the
// allowlist; the core checks again.
func (c *child) handle(l control.Line) {
	if l.M == "" {
		return // the core asks the window nothing, so this is no request
	}
	v, e := control.Check(l.M, l.P, false)
	if e != nil {
		c.w.cfg.Logf("Window: refused %s", Clean(l.M))
		c.fail(l.ID, e)
		return
	}
	if l.M != "hello" && !c.helloed.Load() {
		c.fail(l.ID, &control.Error{Code: control.CodeOrder, Msg: "hello first"})
		return
	}
	core := c.w.cfg.Core
	switch l.M {
	case "hello":
		if h := v.(control.Hello); h.Proto != control.Proto {
			c.rejected.Store(true)
			c.fail(l.ID, &control.Error{Code: control.CodeProto, Msg: "Restart EDSense to finish the update"})
			return
		}
		place := c.w.loadState()
		c.reply(l.ID, control.Welcome{Proto: control.Proto, Version: c.w.cfg.Version, Page: place.Page, Place: place,
			After: c.after})
		if c.helloed.CompareAndSwap(false, true) {
			go c.forwardNotices()
		}
	case "status.get":
		c.reply(l.ID, core.Status())
	case "status.watch":
		if !v.(control.Watch).On {
			c.stopPump()
			c.reply(l.ID, nil)
			return
		}
		c.startPump()
		// answered once the loop has filled the status in, so the page's
		// next status.get shows everything
		go func() {
			for end := time.Now().Add(fullWait); !core.Status().Full && time.Now().Before(end); {
				time.Sleep(5 * time.Millisecond)
			}
			c.reply(l.ID, nil)
		}()
	case "notices.list":
		c.reply(l.ID, orEmpty(core.Notices(0)))
	case "pause.set":
		core.SetPaused(v.(control.Pause).Paused)
		c.reply(l.ID, nil)
	case "demo.play":
		core.PlayDemo()
		c.reply(l.ID, nil)
	case "demo.stop":
		core.StopDemo()
		c.reply(l.ID, nil)
	case "gyro.calibrate":
		core.CalibrateGyro()
		c.reply(l.ID, nil)
	case "file.open":
		go core.OpenFile(v.(control.File).Which)
		c.reply(l.ID, nil)
	case "url.open":
		if address, ok := control.Address(v.(control.URL).ID); ok {
			go core.OpenURL(address)
		}
		c.reply(l.ID, nil)
	case "app.quit":
		c.reply(l.ID, nil)
		go core.Quit()
	case "ui.ready":
		// the window shows right after this, so it counts as seen from now
		c.startPump()
		c.upID.Store(lastNotice(core))
		c.up.Store(true)
		c.reply(l.ID, nil)
	case "ui.closing":
		c.closing.Store(true)
		c.stopPump()
		c.reply(l.ID, nil)
	case "ui.state":
		if err := c.w.saveState(v.(control.UIState)); err != nil {
			c.w.cfg.Logf("Window: %v", err)
			c.fail(l.ID, &control.Error{Code: control.CodeFailed, Msg: err.Error()})
			return
		}
		c.reply(l.ID, nil)
	}
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// fullWait is how long status.watch waits for the loop's first status:
// a few ticks.
const fullWait = 300 * time.Millisecond

// startPump sends the status as it changes, at most 4 times a second.
func (c *child) startPump() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopStatus != nil {
		return
	}
	stop := make(chan struct{})
	c.stopStatus = stop
	c.w.showing.Store(true)
	core := c.w.cfg.Core
	wake, unwatch := core.Watch()
	go func() {
		defer unwatch()
		control.Pump(stop, wake, control.StatusEvery, func() bool {
			st := core.Status()
			if !st.Full {
				return true // the loop's next tick fills it in, and wakes us
			}
			return c.conn.Send(control.Event{Ev: "status", D: st}, true)
		})
	}()
}

func (c *child) stopPump() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopStatus != nil {
		close(c.stopStatus)
		c.stopStatus = nil
		c.w.showing.Store(false)
	}
}

// forwardNotices sends each new notice as it comes.
func (c *child) forwardNotices() {
	core := c.w.cfg.Core
	wake, stop := core.WatchNotices()
	defer stop()
	var last int64
	if all := core.Notices(0); len(all) > 0 {
		last = all[len(all)-1].ID
	}
	for {
		select {
		case <-c.done:
			return
		case <-wake:
		}
		for _, n := range core.Notices(last) {
			if !c.conn.Send(control.Event{Ev: "notice", D: n}, false) {
				return
			}
			last = n.ID
		}
	}
}

// loadState reads ui_state.json; what does not pass the checks is left
// out.
func (w *Window) loadState() control.UIState {
	if w.cfg.StatePath == "" {
		return control.UIState{}
	}
	b, err := os.ReadFile(w.cfg.StatePath)
	if err != nil {
		return control.UIState{}
	}
	var st control.UIState
	if err := json.Unmarshal(b, &st); err != nil {
		w.cfg.Logf("Window: %s: %v", filepath.Base(w.cfg.StatePath), err)
		return control.UIState{}
	}
	return st.Valid()
}

// saveState writes ui_state.json through a temporary file.
func (w *Window) saveState(st control.UIState) error {
	if w.cfg.StatePath == "" {
		return nil
	}
	b, err := json.MarshalIndent(st.Valid(), "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := w.cfg.StatePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("could not save the window's placement: %w", err)
	}
	for i := 0; ; i++ {
		err := os.Rename(tmp, w.cfg.StatePath)
		if err == nil {
			return nil
		}
		if i == 4 {
			os.Remove(tmp)
			return os.WriteFile(w.cfg.StatePath, b, 0o644) // something holds the file: write in place
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// CleanEnv is the environment for the window process: without what could
// steer Wails or WebView2 (the updater helper, a dev server, another
// browser folder or extra browser arguments).
func CleanEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key == "" { // Windows' per-drive folders, "=C:=C:\..."
			out = append(out, kv)
			continue
		}
		k := strings.ToUpper(key)
		if strings.HasPrefix(k, "WAILS_") || strings.HasPrefix(k, "WEBVIEW2_") || strings.HasPrefix(k, "COREWEBVIEW2_") ||
			k == "FRONTEND_DEVSERVER_URL" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// WebViewEnv are the variables Wails' package init sets in every EDSense
// process. Only the window needs them; the core clears them, so the
// programs it opens do not inherit them.
var WebViewEnv = []string{
	"WEBVIEW2_PIPE_FOR_SCRIPT_DEBUGGER", "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
	"WEBVIEW2_RELEASE_CHANNEL_PREFERENCE", "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER", "WEBVIEW2_USER_DATA_FOLDER",
}

// ClearWebViewEnv clears WebViewEnv in this process.
func ClearWebViewEnv() {
	for _, k := range WebViewEnv {
		_ = os.Unsetenv(k)
	}
}
