package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/control"
)

// The test binary also plays the window process: EDSENSE_FAKE_WINDOW says
// how. It speaks the protocol and opens no window.
func TestMain(m *testing.M) {
	if mode := os.Getenv("EDSENSE_FAKE_WINDOW"); mode != "" {
		os.Exit(fakeWindow(mode))
	}
	os.Exit(m.Run())
}

func fakeWindow(mode string) int {
	fmt.Fprintln(os.Stderr, "fake window up")
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		k = strings.ToUpper(k)
		if strings.HasPrefix(k, "WAILS_") || strings.HasPrefix(k, "WEBVIEW2_") || k == "FRONTEND_DEVSERVER_URL" {
			fmt.Fprintln(os.Stderr, "leaked "+k)
		}
	}
	switch mode {
	case "early":
		return 1
	case "sleep": // a process a window left behind
		time.Sleep(30 * time.Second)
		return 0
	case "stuck": // hello, then never reads again
		fmt.Fprintf(os.Stdout, `{"id":1,"m":"hello","p":{"proto":%d}}`+"\n", control.Proto)
		time.Sleep(20 * time.Second)
		return 0
	case "oldproto": // an older or newer exe: hello is refused, the window stays open
		fmt.Fprintf(os.Stdout, `{"id":1,"m":"hello","p":{"proto":%d}}`+"\n", control.Proto+1)
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	}
	conn := control.NewConn(os.Stdout, nil)
	client := control.NewClient(conn)
	events := make(chan control.Line, 64)
	go func() {
		_ = control.Read(os.Stdin, func(l control.Line) {
			if !client.Deliver(l) {
				events <- l
			}
		})
		close(events) // stdin ended
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	call := func(m string, p any) (json.RawMessage, error) { return client.Call(ctx, m, p) }
	if _, err := call("status.get", nil); err != nil {
		fmt.Fprintln(os.Stderr, "status.get before hello refused")
	}
	raw, err := call("hello", control.Hello{Proto: control.Proto})
	if err != nil {
		fmt.Fprintln(os.Stderr, "hello:", err)
		return 2
	}
	var welcome control.Welcome
	_ = json.Unmarshal(raw, &welcome)
	fmt.Fprintf(os.Stderr, "welcome %s %s %d\n", welcome.Version, welcome.Page, welcome.Place.Width)
	fmt.Fprintf(os.Stderr, "after %d\n", welcome.After)
	switch mode {
	case "noshow": // WebView2 did not start
		return 1
	case "closes": // up, then the player closes it
		_, _ = call("ui.ready", nil)
		_, _ = call("status.watch", control.Watch{On: true})
		_, _ = call("ui.closing", nil)
		time.Sleep(300 * time.Millisecond) // Wails' shutdown, the placement
		return 0
	case "orphan": // leaves a process in its job, as WebView2's may be
		exe, _ := os.Executable()
		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), "EDSENSE_FAKE_WINDOW=sleep")
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "grandchild:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "grandchild %d\n", cmd.Process.Pid)
		return 0
	}
	if _, err := call("ui.ready", nil); err != nil {
		fmt.Fprintln(os.Stderr, "ui.ready:", err)
	}
	if _, err := call("status.watch", control.Watch{On: mode != "hidden"}); err != nil {
		fmt.Fprintln(os.Stderr, "watch:", err)
	}
	if _, err := call("shell.run", map[string]string{"cmd": "calc"}); err != nil {
		fmt.Fprintln(os.Stderr, "refused shell.run")
	}
	if _, err := call("url.open", map[string]string{"id": "https://example.com"}); err != nil {
		fmt.Fprintln(os.Stderr, "refused a raw address")
	}
	_, _ = call("pause.set", control.Pause{Paused: true})
	_, _ = call("url.open", control.URL{ID: "source"})
	_, _ = call("file.open", control.File{Which: control.FileLog})
	gotStatus, gotNotice := false, false
	for l := range events {
		switch l.Ev {
		case "status":
			var st control.Status
			_ = json.Unmarshal(l.D, &st)
			if !gotStatus {
				fmt.Fprintf(os.Stderr, "status %s\n", st.Text)
			}
			gotStatus = true
		case "notice":
			var n control.Notice
			_ = json.Unmarshal(l.D, &n)
			fmt.Fprintf(os.Stderr, "notice %s\n", n.Text)
			gotNotice = true
		case "focus":
			fmt.Fprintln(os.Stderr, "focus")
		case "bye":
			_, _ = call("ui.state", control.UIState{X: 5, Y: 6, Width: 900, Height: 650, Page: "about"})
			fmt.Fprintf(os.Stderr, "bye after status=%v notice=%v\n", gotStatus, gotNotice)
			conn.Flush(time.Second)
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "stdin ended")
	return 0
}

// fakeCore records what the window asks for.
type fakeCore struct {
	mu      sync.Mutex
	calls   []string
	notices []control.Notice
	status  control.Status
	wake    chan struct{}
	nwake   chan struct{}
}

func newFakeCore() *fakeCore {
	return &fakeCore{status: control.Status{Full: true, Text: "Waiting for Elite Dangerous", Level: control.LevelIdle},
		wake: make(chan struct{}, 1), nwake: make(chan struct{}, 1)}
}

func (f *fakeCore) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeCore) has(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == s {
			return true
		}
	}
	return false
}

func (f *fakeCore) Status() control.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}
func (f *fakeCore) Watch() (<-chan struct{}, func()) { f.record("watch"); return f.wake, func() {} }
func (f *fakeCore) Notices(after int64) []control.Notice {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []control.Notice
	for _, n := range f.notices {
		if n.ID > after {
			out = append(out, n)
		}
	}
	return out
}
func (f *fakeCore) WatchNotices() (<-chan struct{}, func()) { return f.nwake, func() {} }
func (f *fakeCore) SetPaused(p bool)                        { f.record(fmt.Sprintf("paused %v", p)) }
func (f *fakeCore) PlayDemo()                               { f.record("demo") }
func (f *fakeCore) StopDemo()                               { f.record("demo stop") }
func (f *fakeCore) CalibrateGyro()                          { f.record("calibrate") }
func (f *fakeCore) OpenFile(which string)                   { f.record("file " + which) }
func (f *fakeCore) OpenURL(address string)                  { f.record("url " + address) }
func (f *fakeCore) Quit()                                   { f.record("quit") }

func (f *fakeCore) notice(text string) {
	f.mu.Lock()
	id := int64(len(f.notices) + 1)
	f.notices = append(f.notices, control.Notice{ID: id, Text: text, Level: control.NoticeInfo})
	f.mu.Unlock()
	select {
	case f.nwake <- struct{}{}:
	default:
	}
}

// logs collects the core's log lines.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *logs) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

// after is what follows prefix on the first line that has it.
func (l *logs) after(prefix string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if _, rest, ok := strings.Cut(line, prefix); ok {
			return rest
		}
	}
	return ""
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testWindow(t *testing.T, mode string, core Core) (*Window, *logs, string) {
	t.Helper()
	t.Setenv("EDSENSE_FAKE_WINDOW", mode)
	// what the window must never see
	t.Setenv("WAILS_UPDATER_HELPER", "1")
	t.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--remote-debugging-port=9222")
	t.Setenv("FRONTEND_DEVSERVER_URL", "http://127.0.0.1:5173")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	state := filepath.Join(dir, "ui_state.json")
	l := &logs{}
	w := New(Config{Exe: exe, Args: []string{}, Version: "9.9.9", StatePath: state, Core: core, Logf: l.logf})
	return w, l, state
}

func TestWindowSession(t *testing.T) {
	core := newFakeCore()
	w, l, state := testWindow(t, "normal", core)
	if err := os.WriteFile(state, []byte(`{"x":10,"y":20,"width":1000,"height":700,"page":"home"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	core.notice("Elite Dangerous closed") // before the window: it is not new to it
	w.Open()
	if !w.Running() {
		t.Fatal("not running after Open")
	}
	eventually(t, "the window's calls", func() bool {
		return core.has("paused true") && core.has("url https://github.com/tolgahan/ed-sense") && core.has("file log")
	})

	eventually(t, "the first status", func() bool { return l.has("window: status Waiting for Elite Dangerous") })
	core.notice("DSX connected")
	eventually(t, "the notice", func() bool { return l.has("window: notice DSX connected") })
	if w.Takes(1, 0) || !w.Takes(2, 0) {
		t.Errorf("Takes: the notice before the window %v, after it %v", w.Takes(1, 0), w.Takes(2, 0))
	}
	w.Open() // already open: to the front
	eventually(t, "focus", func() bool { return l.has("window: focus") })
	w.Close(5 * time.Second)
	if w.Running() {
		t.Fatal("still running after Close")
	}
	for _, want := range []string{
		"window: fake window up",
		"window: status.get before hello refused",
		"window: welcome 9.9.9 home 1000",
		"window: after 1",
		"window: refused shell.run",
		"window: refused a raw address",
		"window: bye after status=true notice=true",
		"Window: closed",
	} {
		if !l.has(want) {
			t.Errorf("no %q in the log:\n%s", want, l)
		}
	}
	if l.has("did not close") {
		t.Errorf("the window had to be stopped:\n%s", l)
	}
	if l.has("leaked") {
		t.Errorf("the window saw variables it must not:\n%s", l)
	}
	b, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var st control.UIState
	if err := json.Unmarshal(b, &st); err != nil || st != (control.UIState{X: 5, Y: 6, Width: 900, Height: 650, Page: "about"}) {
		t.Errorf("ui_state.json: %s (%v)", b, err)
	}
	w.Open() // closing: nothing starts
	if w.Running() {
		t.Error("opened after Close")
	}
	if !w.Takes(99, 0) {
		t.Error("a box while EDSense quits")
	}
}

func TestEarlyExitsTellOnce(t *testing.T) {
	core := newFakeCore()
	w, l, _ := testWindow(t, "early", core)
	warned := make(chan string, 4)
	w.cfg.Warn = func(s string) { warned <- s }
	for i := 0; i < 2; i++ {
		w.Open()
		eventually(t, "the exit", func() bool { return !w.Running() })
	}
	select {
	case s := <-warned:
		if s != CouldNotStart {
			t.Errorf("told %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("not told after two early exits:\n%s", l)
	}
	w.Open()
	eventually(t, "the exit", func() bool { return !w.Running() })
	select {
	case s := <-warned:
		t.Errorf("told again after one more exit: %q", s)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestExitAfterHelloTells: WebView2 starts after hello, so a window that
// exits between the two is a failed start. One whose page was up is not.
func TestExitAfterHelloTells(t *testing.T) {
	core := newFakeCore()
	w, l, _ := testWindow(t, "noshow", core)
	warned := make(chan string, 4)
	w.cfg.Warn = func(s string) { warned <- s }
	run := func(mode string) {
		t.Helper()
		t.Setenv("EDSENSE_FAKE_WINDOW", mode)
		w.Open()
		if mode == "normal" {
			eventually(t, "the page", func() bool {
				w.mu.Lock()
				defer w.mu.Unlock()
				return w.cur != nil && w.cur.up.Load()
			})
			w.mu.Lock()
			_ = w.cur.cmd.Process.Kill()
			w.mu.Unlock()
		}
		eventually(t, "the exit", func() bool { return !w.Running() })
	}
	run("noshow")
	run("normal") // its page was up: the count starts again
	run("noshow")
	select {
	case s := <-warned:
		t.Fatalf("told %q after one failed start:\n%s", s, l)
	case <-time.After(200 * time.Millisecond):
	}
	run("noshow")
	select {
	case s := <-warned:
		if s != CouldNotStart {
			t.Errorf("told %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("not told after two failed starts:\n%s", l)
	}
}

// grandchild is the pid of the process an "orphan" window left behind.
func grandchild(l *logs) int {
	pid, _ := strconv.Atoi(strings.TrimSpace(l.after("window: grandchild ")))
	return pid
}

// TestStuckWindowIsClosed: a window that stops reading gets its queue
// full; the core closes it and never waits for it.
func TestStuckWindowIsClosed(t *testing.T) {
	core := newFakeCore()
	w, l, _ := testWindow(t, "stuck", core)
	w.Open()
	eventually(t, "hello", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.cur != nil && w.cur.helloed.Load()
	})
	big := strings.Repeat("x", 4096)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000 && w.Running(); i++ {
			core.notice(big)
			time.Sleep(time.Millisecond)
		}
	}()
	eventually(t, "the stuck window to be closed", func() bool { return !w.Running() })
	<-done
	if !l.has("it stopped reading") {
		t.Errorf("no log line:\n%s", l)
	}
}

func TestMissingRuntime(t *testing.T) {
	core := newFakeCore()
	w, _, _ := testWindow(t, "normal", core)
	asked := make(chan string, 1)
	w.cfg.Missing = func() bool { return true }
	w.cfg.Ask = func(s string) bool { asked <- s; return true }
	w.Open()
	select {
	case s := <-asked:
		if s != NoRuntime {
			t.Errorf("asked %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not asked")
	}
	eventually(t, "the download page", func() bool { return core.has("url " + control.WebView2Download) })
	if w.Running() {
		t.Error("started without a runtime")
	}
}

func TestCleanEnv(t *testing.T) {
	in := []string{"PATH=C:\\Windows", "=C:=C:\\x", "wails_updater_helper=1", "WAILS_TARGET=x", "WebView2_User_Data_Folder=y",
		"COREWEBVIEW2_FORCED_HOSTING_MODE=1", "FRONTEND_DEVSERVER_URL=http://x", "EDSENSE=1", "WAILSX=keep"}
	got := CleanEnv(in)
	want := []string{"PATH=C:\\Windows", "=C:=C:\\x", "EDSENSE=1", "WAILSX=keep"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("CleanEnv = %q", got)
	}
}

func TestClean(t *testing.T) {
	if got := Clean("a\rb\x1b[31mc\x7f"); got != "a b [31mc " {
		t.Errorf("Clean = %q", got)
	}
	if got := Clean(strings.Repeat("x", 1000)); len(got) != 403 {
		t.Errorf("long line kept %d bytes", len(got))
	}
}

func TestRuntimeVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"129.0.2792.65": true, "86.0.616.0": true, "86.0.615.9": false, "85.9.9.9": false,
		"0.0.0.0": false, "": false, "abc": false, "1.2.3.4.5": false, "200": true,
	} {
		if got := usableRuntime(v); got != want {
			t.Errorf("usableRuntime(%q) = %v", v, got)
		}
	}
}

func TestStatusOf(t *testing.T) {
	w := backend.DSXWords()
	l := app.Live{Status: app.Status{Backend: "DSX", Online: true, EliteRunning: true, Active: true, Context: "supercruise"},
		Haptics: app.HapticsNative, GyroAim: true, GyroBy: "edsense", HasGyro: true, GyroAiming: true, Shield: 80, Heat: -1, FireGroup: 2}
	st := StatusOf(l, w, "127.0.0.1:6969")
	if st.Level != control.LevelActive || st.Text != "Active: supercruise" || st.Addr != "127.0.0.1:6969" ||
		st.Haptics != control.HapticsNative || !st.Gyro.Aiming || st.HUD.Shield != 80 || st.HUD.Heat != -1 || st.FireGroup != 2 {
		t.Errorf("StatusOf = %+v", st)
	}
	st = StatusOf(app.Live{Status: app.Status{}}, w, "")
	if st.Level != control.LevelError || st.Text != w.TrayOffline || st.Backend != "DSX" || st.Haptics != control.HapticsWaiting {
		t.Errorf("offline: %+v", st)
	}
}

// TestTakes: the window shows the messages from its start on once its
// page is up, and later ones only while it can be seen. One that never
// comes up, or speaks another protocol, shows none.
func TestTakes(t *testing.T) {
	t.Run("gone", func(t *testing.T) {
		w, _, _ := testWindow(t, "noshow", newFakeCore())
		if w.Takes(1, 0) {
			t.Error("no window, but it takes a message")
		}
		w.Open()
		if w.Takes(1, 10*time.Second) {
			t.Error("a window that exited before its page came up took a message")
		}
		w.Close(time.Second)
	})
	t.Run("hidden", func(t *testing.T) {
		core := newFakeCore()
		w, l, _ := testWindow(t, "hidden", core)
		core.notice("before")
		w.Open()
		core.notice("while it starts") // 2: its page shows it as it comes up
		if !w.Takes(2, 10*time.Second) {
			t.Errorf("a message from its start was not taken:\n%s", l)
		}
		eventually(t, "the hidden page", func() bool { return core.has("paused true") && !w.Showing() })
		core.notice("while hidden") // 3: a box
		if w.Takes(1, 0) || w.Takes(3, 0) {
			t.Errorf("Takes before the start %v, while hidden %v", w.Takes(1, 0), w.Takes(3, 0))
		}
		w.Close(5 * time.Second)
	})
	t.Run("closing", func(t *testing.T) {
		core := newFakeCore()
		w, l, _ := testWindow(t, "closes", core)
		w.Open()
		eventually(t, "the window going", func() bool {
			w.mu.Lock()
			defer w.mu.Unlock()
			return w.cur != nil && w.cur.closing.Load()
		})
		core.notice("after the close") // 1: a box, as no page shows it
		if w.Showing() || w.Takes(1, 0) {
			t.Errorf("a window that is going: showing %v, takes %v", w.Showing(), w.Takes(1, 0))
		}
		w.Open() // a click on the tray icon now: a new window once this one has ended
		eventually(t, "the next window", func() bool { return strings.Count(l.String(), "window: fake window up") == 2 })
		w.Close(5 * time.Second)
	})
	t.Run("oldproto", func(t *testing.T) {
		w, _, _ := testWindow(t, "oldproto", newFakeCore())
		w.Open()
		start := time.Now()
		if w.Takes(1, 10*time.Second) {
			t.Error("a window with another protocol took a message")
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("waited %v for a window with another protocol", d)
		}
		w.Close(5 * time.Second)
	})
}
