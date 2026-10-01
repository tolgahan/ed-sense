package launch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/engine"
)

// reader is the core's side of a window run in this process: lines go to
// handle as the reader passes them, and what the core sends comes back.
type reader struct {
	t     *testing.T
	c     *child
	log   *logs
	lines chan control.Line
}

func newReader(t *testing.T, core Core) *reader {
	t.Helper()
	pr, pw := io.Pipe()
	l := &logs{}
	c := &child{w: New(Config{Core: core, Logf: l.logf}), done: make(chan struct{})}
	c.conn = control.NewConn(pw, nil)
	r := &reader{t: t, c: c, log: l, lines: make(chan control.Line, 64)}
	go func() { _ = control.Read(pr, func(l control.Line) { r.lines <- l }) }()
	t.Cleanup(func() {
		close(c.done)
		c.conn.Close()
		pw.Close()
	})
	r.ask(1, "hello", fmt.Sprintf(`{"proto":%d}`, control.Proto))
	if got := r.next(); got.ID != 1 || got.Err != nil {
		t.Fatalf("hello: %+v", got)
	}
	return r
}

// ask hands the core one request, as its reader does.
func (r *reader) ask(id int64, m, p string) {
	var raw json.RawMessage
	if p != "" {
		raw = json.RawMessage(p)
	}
	r.c.handle(control.Line{ID: id, M: m, P: raw})
}

// next is the next line the core sends.
func (r *reader) next() control.Line {
	r.t.Helper()
	select {
	case l := <-r.lines:
		return l
	case <-time.After(5 * time.Second):
		r.t.Fatal("the core sent nothing")
	}
	return control.Line{}
}

// none: the core sends nothing for a while.
func (r *reader) none(what string) {
	r.t.Helper()
	select {
	case l := <-r.lines:
		r.t.Errorf("%s: the core sent %+v", what, l)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestSlowCallsDoNotWait: whatever may wait (a switch, a probe, a write)
// answers from a goroutine of its own, so the reader goes on and a
// pause.set right after is answered first.
func TestSlowCallsDoNotWait(t *testing.T) {
	core := newFakeCore()
	r := newReader(t, core)
	core.block = make(chan struct{})
	slow := []struct{ m, p string }{
		{"settings.patch", `{"patch":{"poll_ms":20}}`},
		{"engine.apply", ``},
		{"backend.choose", `{"choice":"ds4windows"}`},
		{"backend.detect", `{"fresh":true}`},
		{"setup.check", `{"app":"ds4windows","fresh":true}`},
		{"profile.reset", `{"app":"dsx"}`},
	}
	asked := make(chan struct{})
	go func() {
		defer close(asked)
		for i, s := range slow {
			r.ask(int64(10+i), s.m, s.p)
		}
		r.ask(30, "ui.state", `{"page":"controller"}`)
		r.ask(40, "pause.set", `{"paused":true}`)
	}()
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Error("the reader waited for a slow call")
		close(core.block)
		<-asked
		return
	}
	first := map[int64]bool{}
	for range 2 {
		got := r.next()
		first[got.ID] = got.Err == nil
	}
	if !first[30] || !first[40] {
		t.Errorf("first answers %v, want ui.state's and pause.set's", first)
	}
	if !core.has("paused true") {
		t.Error("pause.set did not reach the core")
	}
	r.none("while the slow calls wait")
	close(core.block)
	got := map[int64]string{}
	for range slow {
		l := r.next()
		if l.Err != nil {
			t.Errorf("%d: %v", l.ID, l.Err)
		}
		got[l.ID] = string(l.OK)
	}
	want := map[int64]string{
		10: `{"applied":true,"rev":2,"problems":[]}`,
		11: `{"choice":"auto","pinned":false,"kind":"dsx","name":"DSX","why":"","addr":"","switching":false,"pending":[]}`,
		12: `{"choice":"auto","pinned":false,"kind":"dsx","name":"DSX","why":"","addr":"","switching":false,"pending":[]}`,
		13: `{"t":7,"dsx":{"running":false,"addr":"","answers":""},"ds4windows":{"running":false,"addr":"","answers":""},"auto":{"kind":"","why":"","sure":false}}`,
		14: `{"app":"ds4windows","t":7,"items":[{"id":"app","state":"ok","text":"DSX runs"}]}`,
		15: `{"app":"dsx","state":"waiting","text":` + quoted(ResetWaits) + `}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("answers\n%v\nwant\n%v", got, want)
	}
	for _, c := range []string{`patch {"poll_ms":20}`, "apply", "choose ds4windows", "detect true", "check ds4windows true", "reset dsx"} {
		if !core.has(c) {
			t.Errorf("the core was not asked %q", c)
		}
	}
}

// quoted is s as JSON writes it.
func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestCallErrors: what the page is told when the engine or the file
// says no; a choice that works but is not saved is answered with what
// runs, and logged once, as the tray does.
func TestCallErrors(t *testing.T) {
	core := newFakeCore()
	r := newReader(t, core)
	denied := &fs.PathError{Op: "open", Path: filepath.Join("C:", "Users", "someone", "EDSense", "edsense.json.tmp"), Err: errors.New("Access is denied.")}
	cases := []struct {
		err      error
		m, p     string
		code     string
		msg      string
		notSaved string
		log      string
	}{
		{engine.ErrBusy, "engine.apply", ``, control.CodeBusy, "busy", "", "Apply now: busy"},
		{fmt.Errorf("%w: edsense.json line 2, column 5: invalid character", engine.ErrBroken), "backend.choose", `{"choice":"dsx"}`,
			control.CodeBroken, "the settings file has an error: edsense.json line 2, column 5: invalid character", "",
			"Controller app: the settings file has an error: edsense.json line 2, column 5: invalid character"},
		{errors.New("no socket"), "backend.choose", `{"choice":"dsx"}`, control.CodeFailed, "no socket", "", "Controller app: no socket"},
		{engine.ErrStopped, "engine.apply", ``, control.CodeFailed, "stopped", "", "Apply now: stopped"},
		{&engine.NotSavedError{Err: denied}, "backend.choose", `{"choice":"ds4windows"}`, "", "", "open edsense.json.tmp: Access is denied.",
			"Controller app: changed for this run only, not saved: open " + denied.Path + ": Access is denied."},
		{denied, "settings.patch", `{"patch":{"poll_ms":20}}`, control.CodeFailed, "open edsense.json.tmp: Access is denied.", "",
			"Settings not changed in the window: open " + denied.Path + ": Access is denied."},
	}
	for i, c := range cases {
		core.mu.Lock()
		core.err = c.err
		core.mu.Unlock()
		r.log.mu.Lock()
		r.log.lines = nil
		r.log.mu.Unlock()
		r.ask(int64(100+i), c.m, c.p)
		l := r.next()
		switch {
		case c.code != "":
			if l.Err == nil || l.Err.Code != c.code || l.Err.Msg != c.msg {
				t.Errorf("%d %s: %+v, want %s %q", i, c.m, l.Err, c.code, c.msg)
			}
		default:
			var e control.Engine
			if l.Err != nil || json.Unmarshal(l.OK, &e) != nil || e.NotSaved != c.notSaved || e.Kind != "dsx" {
				t.Errorf("%d %s: %s %+v", i, c.m, l.OK, l.Err)
			}
		}
		if got := r.log.String(); got != c.log {
			t.Errorf("%d %s logged %q, want %q", i, c.m, got, c.log)
		}
		if strings.Contains(string(l.OK)+fmt.Sprint(l.Err), "someone") {
			t.Errorf("%d %s: the window got a full path", i, c.m)
		}
	}
}

// TestForwardSettings: once hello is answered, each new settings rev goes
// to the window as a config event, the newest only and never twice.
func TestForwardSettings(t *testing.T) {
	core := newFakeCore()
	r := newReader(t, core)
	core.newSettings(1) // not newer than what the window can ask for
	r.none("the same rev")
	core.newSettings(2)
	l := r.next()
	var s control.Settings
	if l.Ev != "config" || json.Unmarshal(l.D, &s) != nil || s.Rev != 2 || s.Config == nil {
		t.Fatalf("config event %+v", l)
	}
	core.mu.Lock()
	core.settings.Rev = 3
	core.settings.Rev = 4
	core.mu.Unlock()
	core.newSettings(4)
	l = r.next()
	if json.Unmarshal(l.D, &s) != nil || s.Rev != 4 {
		t.Errorf("after two changes: %s", l.D)
	}
	r.none("nothing new")

	// a second hello starts no second forwarder
	r.ask(5, "hello", fmt.Sprintf(`{"proto":%d}`, control.Proto))
	r.next()
	core.newSettings(5)
	r.next()
	r.none("one forwarder")
}

// stuckWriter never returns from Write until released: a window that
// stopped reading. entered is closed when the first write starts, so the
// queue behind it can be filled for sure.
type stuckWriter struct {
	release, entered chan struct{}
	once             *sync.Once
}

func newStuckWriter() stuckWriter {
	return stuckWriter{release: make(chan struct{}), entered: make(chan struct{}), once: new(sync.Once)}
}

func (s stuckWriter) Write(b []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return len(b), nil
}

// TestSettingsNotDropped: a config event that finds the window's queue
// full is never dropped, as a status is: the window is closed instead.
func TestSettingsNotDropped(t *testing.T) {
	core := newFakeCore()
	w := newStuckWriter()
	defer close(w.release)
	overflow := make(chan struct{})
	var once sync.Once
	c := &child{w: New(Config{Core: core, Logf: (&logs{}).logf}), done: make(chan struct{})}
	defer close(c.done)
	c.conn = control.NewConn(w, func() { once.Do(func() { close(overflow) }) })
	defer c.conn.Close()
	c.handle(control.Line{ID: 1, M: "hello", P: json.RawMessage(fmt.Sprintf(`{"proto":%d}`, control.Proto))})
	<-w.entered // the hello reply holds the writer: nothing leaves the queue now
	for c.conn.Send(control.Event{Ev: "status"}, true) {
	}
	core.newSettings(2)
	select {
	case <-overflow:
	case <-time.After(5 * time.Second):
		t.Fatal("a config event was dropped")
	}
}

// TestAppCoreSettings: the window's view of the Store, and its patches.
func TestAppCoreSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edsense.json")
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatal(err)
	}
	store, _, err := config.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	c := &AppCore{Store: store}
	s := c.Settings()
	if s.Rev != 1 || !s.FirstRun || s.Broken != nil {
		t.Errorf("settings %+v", s)
	}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"poll_ms":`) || !strings.Contains(string(b), `"broken":null`) {
		t.Errorf("settings JSON %s", b)
	}
	wake, stop := c.WatchSettings()
	defer stop()

	p, err := c.PatchSettings([]byte(`{"poll_ms":20,"nope":1}`))
	if err != nil || p.Applied || len(p.Problems) != 1 || p.Problems[0].Path != "nope" || p.Problems[0].Code != "unknown" {
		t.Errorf("refused patch %+v, %v", p, err)
	}
	p, err = c.PatchSettings([]byte(`{"poll_ms":20}`))
	if err != nil || !p.Applied || p.Rev != 2 || p.Problems == nil || len(p.Problems) != 0 {
		t.Errorf("patch %+v, %v", p, err)
	}
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Error("a patch did not wake the settings watcher")
	}
	if s := c.Settings(); s.Rev != 2 {
		t.Errorf("after the patch: rev %d", s.Rev)
	}
	if b, _ := json.Marshal(c.Schema()); !strings.Contains(string(b), `"path":"poll_ms"`) {
		t.Error("the schema has no poll_ms")
	}

	// the file breaks: the window hears where
	if err := os.WriteFile(path, []byte("{\n  \"poll_ms\": ,\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Reload(); err == nil {
		t.Fatal("a broken file reloaded")
	}
	s = c.Settings()
	if s.Broken == nil || s.Broken.Line != 2 || s.Broken.Msg == "" || s.Rev != 3 {
		t.Errorf("broken: %+v %+v", s, s.Broken)
	}
	p, err = c.PatchSettings([]byte(`{"poll_ms":30}`))
	if err != nil || p.Applied || len(p.Problems) != 1 || p.Problems[0].Code != config.CodeBroken {
		t.Errorf("patch of a broken file %+v, %v", p, err)
	}
}

// TestEngineViews: what the window shows of the engine.
func TestEngineViews(t *testing.T) {
	e := engine.State{Choice: "auto", Kind: backend.KindDS4Windows, Name: "DS4Windows", Why: "auto: DS4Windows runs",
		Addr: "127.0.0.1:6969", Switching: true}
	got := EngineOf(e)
	want := control.Engine{Choice: "auto", Kind: "ds4windows", Name: "DS4Windows", Why: "auto: DS4Windows runs",
		Addr: "127.0.0.1:6969", Switching: true, Pending: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EngineOf = %+v", got)
	}
	st := StatusOf(app.Live{}, backend.DSXWords(), "")
	e.Pinned, e.Pending = true, []string{"poll_ms"}
	WithEngine(&st, e)
	if st.Kind != "ds4windows" || st.Choice != "auto" || !st.Pinned || st.Why != e.Why || !st.Switching ||
		st.Addr != e.Addr || !reflect.DeepEqual(st.Pending, []string{"poll_ms"}) {
		t.Errorf("WithEngine = %+v", st)
	}

	d := engine.Detection{T: 9,
		DSX:  engine.DSXSeen{Running: true, Addr: "127.0.0.1:6969", Answers: "ds4windows"},
		DS4W: engine.DS4WSeen{Running: true, Version: "5.0.12.0", Addr: "127.0.0.1:6969", Answers: "ds4windows", Dir: true},
		Auto: engine.AutoPick{Kind: backend.KindDS4Windows, Why: "DS4Windows answers on its port", Sure: true}}
	wantD := control.Detection{T: 9,
		DSX:        control.Seen{Running: true, Addr: "127.0.0.1:6969", Answers: "ds4windows"},
		DS4Windows: control.Seen{Running: true, Version: "5.0.12.0", Addr: "127.0.0.1:6969", Answers: "ds4windows", Dir: true},
		Auto:       control.AutoPick{Kind: "ds4windows", Why: "DS4Windows answers on its port", Sure: true}}
	if got := DetectionOf(d); got != wantD {
		t.Errorf("DetectionOf = %+v", got)
	}
}

// TestChoicesMatch: the choices the protocol takes are the settings'.
func TestChoicesMatch(t *testing.T) {
	for _, pair := range [][2]string{
		{control.AppAuto, config.BackendAuto},
		{control.AppDSX, config.BackendDSX},
		{control.AppDS4Windows, config.BackendDS4Windows},
		{control.AppDSX, string(backend.KindDSX)},
		{control.AppDS4Windows, string(backend.KindDS4Windows)},
	} {
		if pair[0] != pair[1] {
			t.Errorf("the protocol says %q, the settings %q", pair[0], pair[1])
		}
	}
	for _, c := range []string{config.BackendAuto, config.BackendDSX, config.BackendDS4Windows} {
		if _, err := control.Check("backend.choose", json.RawMessage(`{"choice":"`+c+`"}`), true); err != nil {
			t.Errorf("%s: %v", c, err)
		}
	}
}

// TestPlain: the window never gets a full path.
func TestPlain(t *testing.T) {
	dir := filepath.Join("C:", "Users", "someone", "EDSense")
	for _, c := range []struct {
		err  error
		want string
	}{
		{errors.New("no socket"), "no socket"},
		{fmt.Errorf("save: %w", &fs.PathError{Op: "open", Path: filepath.Join(dir, "edsense.json"), Err: errors.New("denied")}), "save: open edsense.json: denied"},
		{&os.LinkError{Op: "rename", Old: filepath.Join(dir, "a.tmp"), New: filepath.Join(dir, "a.json"), Err: errors.New("in use")}, "rename a.tmp a.json: in use"},
	} {
		if got := Plain(c.err); got != c.want {
			t.Errorf("Plain(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if e := ErrorOf(fmt.Errorf("x: %w", engine.ErrBusy)); e.Code != control.CodeBusy {
		t.Errorf("busy: %+v", e)
	}
}

// pickEngine records which of ChooseWait and SaveWait AppCore calls.
type pickEngine struct {
	Engine
	calls []string
}

func (p *pickEngine) ChooseWait(choice, from string, wait time.Duration) (engine.State, error) {
	p.calls = append(p.calls, fmt.Sprintf("choose %s %s %v", choice, from, wait))
	return engine.State{Choice: choice}, nil
}

func (p *pickEngine) SaveWait(choice, from string, wait time.Duration) (engine.State, error) {
	p.calls = append(p.calls, fmt.Sprintf("save %s %s %v", choice, from, wait))
	return engine.State{Choice: "dsx", Pinned: true}, nil
}

// TestChooseKeepPin: the first run's "Set up later" asks the engine only
// to save while -backend decides this run; every other choice switches.
func TestChooseKeepPin(t *testing.T) {
	core := newFakeCore()
	r := newReader(t, core)
	r.ask(1, "backend.choose", `{"choice":"auto","keep_pin":true}`)
	r.ask(2, "backend.choose", `{"choice":"auto"}`)
	for range 2 {
		if l := r.next(); l.Err != nil {
			t.Errorf("%d: %v", l.ID, l.Err)
		}
	}
	if !core.has("choose auto keep_pin") || !core.has("choose auto") {
		t.Errorf("asked %q", core.calls)
	}

	eng := &pickEngine{}
	c := &AppCore{Engine: eng}
	if e, err := c.Choose("auto", true); err != nil || !e.Pinned {
		t.Errorf("keep_pin: %+v %v", e, err)
	}
	if _, err := c.Choose("ds4windows", false); err != nil {
		t.Error(err)
	}
	want := []string{"save auto window " + OpWait.String(), "choose ds4windows window " + OpWait.String()}
	if !reflect.DeepEqual(eng.calls, want) {
		t.Errorf("engine calls %q, want %q", eng.calls, want)
	}
}
