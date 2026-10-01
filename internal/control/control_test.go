package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	cases := []struct {
		m, p     string
		fromPage bool
		code     string // "" for allowed
	}{
		{"hello", `{"proto":2}`, false, ""},
		{"hello", `{"proto":2}`, true, CodeUnknown}, // the window process's own
		{"ui.state", `{"x":10,"y":20,"width":1040,"height":720,"page":"home"}`, false, ""},
		{"ui.state", `{"page":"home"}`, true, CodeUnknown},
		{"ui.state", `{"x":10,"y":20,"width":5,"height":720}`, false, CodeParams},
		{"ui.state", `{"page":"../etc"}`, false, CodeParams},
		{"ui.ready", ``, false, ""},
		{"ui.ready", ``, true, CodeUnknown},
		{"ui.closing", ``, false, ""},
		{"ui.closing", ``, true, CodeUnknown},
		{"status.get", ``, true, ""},
		{"status.get", `{}`, true, ""},
		{"status.get", `{"x":1}`, true, CodeParams},
		{"status.watch", `{"on":true}`, true, ""},
		{"status.watch", `{"on":true,"detail":1}`, true, CodeParams},
		{"pause.set", `{"paused":true}`, true, ""},
		{"pause.set", `{"paused":"yes"}`, true, CodeParams},
		{"pause.set", `{"paused":true}{"paused":false}`, true, CodeParams},
		{"file.open", `{"which":"settings"}`, true, ""},
		{"file.open", `{"which":"log"}`, true, ""},
		{"file.open", `{"which":"C:\\Windows\\win.ini"}`, true, CodeParams},
		{"file.open", `{"path":"x"}`, true, CodeParams},
		{"url.open", `{"id":"source"}`, true, ""},
		{"url.open", `{"id":"https://example.com"}`, true, CodeParams},
		{"url.open", `{"url":"https://example.com"}`, true, CodeParams},
		{"demo.play", ``, true, ""},
		{"demo.stop", ``, true, ""},
		{"gyro.calibrate", ``, true, ""},
		{"notices.list", ``, true, ""},
		{"app.quit", ``, true, ""},
		{"settings.get", ``, true, ""},
		{"settings.get", `{"rev":1}`, true, CodeParams},
		{"settings.schema", ``, true, ""},
		{"settings.patch", `{"patch":{"poll_ms":20}}`, true, ""},
		{"settings.patch", `{"patch":{}}`, true, ""},
		{"settings.patch", ` {"patch": {"rumble":{"hit":null}}} `, true, ""},
		{"settings.patch", `{}`, true, CodeParams},
		{"settings.patch", `{"patch":null}`, true, CodeParams},
		{"settings.patch", `{"patch":[1]}`, true, CodeParams},
		{"settings.patch", `{"patch":"poll_ms"}`, true, CodeParams},
		{"settings.patch", `{"patch":{},"path":"C:\\x"}`, true, CodeParams},
		{"settings.patch", `{"patch":{`, true, CodeParams},
		{"engine.apply", ``, true, ""},
		{"engine.apply", `{"why":"x"}`, true, CodeParams},
		{"backend.choose", `{"choice":"auto"}`, true, ""},
		{"backend.choose", `{"choice":"dsx"}`, true, ""},
		{"backend.choose", `{"choice":"ds4windows"}`, true, ""},
		{"backend.choose", `{"choice":""}`, true, CodeParams},
		{"backend.choose", `{"choice":"DSX"}`, true, CodeParams},
		{"backend.choose", `{"choice":"xbox"}`, true, CodeParams},
		{"backend.choose", ``, true, CodeParams},
		{"backend.choose", `{"choice":"auto","keep_pin":true}`, true, ""},
		{"backend.choose", `{"choice":"auto","keep_pin":"yes"}`, true, CodeParams},
		{"backend.choose", `{"choice":"auto","pin":true}`, true, CodeParams},
		{"backend.detect", ``, true, ""},
		{"backend.detect", `{"fresh":true}`, true, ""},
		{"backend.detect", `{"fresh":"yes"}`, true, CodeParams},
		{"setup.check", `{"app":"dsx"}`, true, ""},
		{"setup.check", `{"app":"ds4windows","fresh":true}`, true, ""},
		{"setup.check", `{"app":"auto"}`, true, CodeParams},
		{"setup.check", ``, true, CodeParams},
		{"profile.reset", `{"app":"dsx"}`, true, CodeUnknown}, // W2a's, gone
		{"profile.state", `{"app":"dsx"}`, true, ""},
		{"profile.state", `{"app":"ds4windows"}`, true, ""},
		{"profile.state", `{"app":"auto"}`, true, CodeParams},
		{"profile.state", `{"app":"DSX"}`, true, CodeParams},
		{"profile.state", ``, true, CodeParams},
		{"profile.state", `{"app":"dsx","reset":true}`, true, CodeParams},
		{"profile.install", `{"app":"dsx"}`, true, ""},
		{"profile.install", `{"app":"ds4windows","reset":true}`, true, ""},
		{"profile.install", `{"app":"dsx","key":"dsx|D|add"}`, true, ""},
		{"profile.install", `{"app":"dsx","key":"` + strings.Repeat("k", 4097) + `"}`, true, CodeParams},
		{"profile.install", `{"app":"dsx","key":7}`, true, CodeParams},
		{"profile.install", `{"app":"ds4windows","reset":"yes"}`, true, CodeParams},
		{"profile.install", `{"app":"ds4windows","dir":"C:/x"}`, true, CodeParams},
		{"profile.install", `{"app":"xbox"}`, true, CodeParams},
		{"profile.install", ``, true, CodeParams},
		{"profile.cancel", `{"app":"ds4windows"}`, true, ""},
		{"profile.cancel", `{"app":""}`, true, CodeParams},
		{"folder.open", `{"which":"dsx_backups"}`, true, ""},
		{"folder.open", `{"which":"ds4windows_backups"}`, true, ""},
		{"folder.open", `{"which":"C:/Windows"}`, true, CodeParams},
		{"folder.open", `{"which":"settings"}`, true, CodeParams},
		{"folder.open", `{"path":"x"}`, true, CodeParams},
		{"folder.open", ``, true, CodeParams},
		{"shell.run", `{"cmd":"calc"}`, true, CodeUnknown},
		{"", ``, true, CodeUnknown},
	}
	for _, c := range cases {
		_, err := Check(c.m, json.RawMessage(c.p), c.fromPage)
		got := ""
		if err != nil {
			got = err.Code
		}
		if got != c.code {
			t.Errorf("Check(%q, %s, page=%v) = %q, want %q", c.m, c.p, c.fromPage, got, c.code)
		}
	}
}

func TestCheckParams(t *testing.T) {
	v, err := Check("pause.set", json.RawMessage(`{"paused":true}`), true)
	if err != nil || !v.(Pause).Paused {
		t.Fatalf("pause.set = %v, %v", v, err)
	}
	v, err = Check("url.open", json.RawMessage(`{"id":"webview2"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := Address(v.(URL).ID); u != WebView2Download {
		t.Errorf("webview2 link = %q", u)
	}
	for id, want := range map[string]string{
		"ds4windows_doc":      "https://github.com/tolgahan/ed-sense/blob/main/docs/ds4windows.md",
		"ds4windows_releases": "https://github.com/hbashton/DS4Windows/releases",
	} {
		if u, ok := Address(id); !ok || u != want {
			t.Errorf("%s link = %q", id, u)
		}
	}
	v, err = Check("backend.choose", json.RawMessage(`{"choice":"ds4windows"}`), true)
	if err != nil || v.(Choice).Choice != AppDS4Windows || v.(Choice).KeepPin {
		t.Errorf("backend.choose = %v, %v", v, err)
	}
	v, err = Check("backend.choose", json.RawMessage(`{"choice":"auto","keep_pin":true}`), true)
	if err != nil || v.(Choice) != (Choice{Choice: AppAuto, KeepPin: true}) {
		t.Errorf("backend.choose keep_pin = %v, %v", v, err)
	}
	v, err = Check("setup.check", json.RawMessage(`{"app":"dsx","fresh":true}`), true)
	if err != nil || v.(SetupCheck) != (SetupCheck{App: AppDSX, Fresh: true}) {
		t.Errorf("setup.check = %v, %v", v, err)
	}
	v, err = Check("backend.detect", nil, true)
	if err != nil || v.(Detect).Fresh {
		t.Errorf("backend.detect without params = %v, %v", v, err)
	}
	v, err = Check("profile.install", json.RawMessage(`{"app":"ds4windows","reset":true,"key":"k"}`), true)
	if err != nil || v.(ProfileInstall) != (ProfileInstall{App: AppDS4Windows, Reset: true, Key: "k"}) {
		t.Errorf("profile.install = %v, %v", v, err)
	}
	v, err = Check("profile.state", json.RawMessage(`{"app":"dsx"}`), true)
	if err != nil || v.(ProfileApp).App != AppDSX {
		t.Errorf("profile.state = %v, %v", v, err)
	}
	v, err = Check("profile.cancel", json.RawMessage(`{"app":"ds4windows"}`), true)
	if err != nil || v.(ProfileApp).App != AppDS4Windows {
		t.Errorf("profile.cancel = %v, %v", v, err)
	}
	v, err = Check("folder.open", json.RawMessage(`{"which":"ds4windows_backups"}`), true)
	if err != nil || v.(Folder).Which != FolderDS4WindowsBackups {
		t.Errorf("folder.open = %v, %v", v, err)
	}
}

// TestPatchLimit: a settings patch is one object of at most MaxPatch.
func TestPatchLimit(t *testing.T) {
	patch := func(n int) json.RawMessage {
		// the object {"a":"xxx"} is n bytes long
		return json.RawMessage(`{"patch":{"a":"` + strings.Repeat("x", n-8) + `"}}`)
	}
	v, err := Check("settings.patch", patch(MaxPatch), true)
	if err != nil {
		t.Fatalf("a patch of %d bytes: %v", MaxPatch, err)
	}
	if n := len(v.(Patch).Patch); n != MaxPatch {
		t.Errorf("the patch is %d bytes", n)
	}
	if _, err := Check("settings.patch", patch(MaxPatch+1), true); err == nil || err.Code != CodeParams {
		t.Errorf("a patch over the limit: %v", err)
	}

	// B checks the page's bytes, then sends them on through the Client,
	// which escapes <, >, & and U+2028: A must come to the same verdict
	var mu sync.Mutex
	verdicts := map[int64]bool{}
	client := pipePair(t, func(l Line, c *Conn) {
		_, err := Check(l.M, l.P, false)
		mu.Lock()
		verdicts[l.ID] = err == nil
		mu.Unlock()
		c.Send(Reply{ID: l.ID}, false)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	escaped := func(n int, fill string) json.RawMessage {
		// {"a":"<<<"} is n bytes long once each fill is 6 bytes
		return json.RawMessage(`{"patch":{"a":"` + strings.Repeat(fill, (n-8)/6) + `"}}`)
	}
	for i, c := range []struct {
		p    json.RawMessage
		want bool
	}{
		{patch(MaxPatch), true},
		{patch(MaxPatch + 1), false},
		{escaped(MaxPatch-2, "<"), true}, // 10,929 bytes from the page
		{escaped(MaxPatch+4, "<"), false},
		{escaped(MaxPatch+4, "&"), false},
		{escaped(MaxPatch+4, "\u2028"), false},
		{json.RawMessage(`{"patch": {"a" : 1}}`), true},
	} {
		_, berr := Check("settings.patch", c.p, true)
		if (berr == nil) != c.want {
			t.Errorf("%d: B says %v, want ok %v", i, berr, c.want)
			continue
		}
		if berr != nil {
			continue // B refuses it: A never sees it
		}
		if _, err := client.Call(ctx, "settings.patch", c.p); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := verdicts[int64(len(verdicts))]
		mu.Unlock()
		if !got {
			t.Errorf("%d: B took it, A refused it", i)
		}
	}
	// A over B's verdict for the escaped ones, which B refused
	for _, fill := range []string{"<", "&", "\u2028"} {
		sent, _ := json.Marshal(escaped(MaxPatch+4, fill))
		if _, err := Check("settings.patch", sent, false); err == nil {
			t.Errorf("A took %q-heavy params B refused", fill)
		}
	}
}

// TestEvents: what the core may send the window.
func TestEvents(t *testing.T) {
	for ev, want := range map[string]bool{"status": true, "notice": true, "config": true, "focus": true, "bye": true,
		"profile": true, "settings": false, "": false} {
		if KnownEvent(ev) != want {
			t.Errorf("KnownEvent(%q) = %v", ev, !want)
		}
	}
}

// TestPayloads: the shapes the page reads.
func TestPayloads(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{Settings{Rev: 3, Config: map[string]int{"poll_ms": 16}}, `{"rev":3,"config":{"poll_ms":16},"broken":null,"first_run":false}`},
		{Settings{Rev: 4, Broken: &Broken{Line: 2, Col: 5, Msg: "x"}, FirstRun: true},
			`{"rev":4,"config":null,"broken":{"line":2,"col":5,"key":"","msg":"x"},"first_run":true}`},
		{Patched{Applied: true, Rev: 2, Problems: []Problem{}}, `{"applied":true,"rev":2,"problems":[]}`},
		{Engine{Choice: AppAuto, Kind: AppDSX, Name: "DSX", Pending: []string{"poll_ms"}},
			`{"choice":"auto","pinned":false,"kind":"dsx","name":"DSX","why":"","addr":"","switching":false,"pending":["poll_ms"]}`},
		{Engine{Pending: []string{}, NotSaved: "access denied"},
			`{"choice":"","pinned":false,"kind":"","name":"","why":"","addr":"","switching":false,"pending":[],"not_saved":"access denied"}`},
		{Item{ID: "app", State: ItemOK, Text: "DSX runs"}, `{"id":"app","state":"ok","text":"DSX runs"}`},
		{Item{ID: "listener", State: ItemBad, Text: "x", How: "y", Link: "ds4windows_doc"},
			`{"id":"listener","state":"bad","text":"x","how":"y","link":"ds4windows_doc"}`},
		{Detection{T: 1, DS4Windows: Seen{Running: true, Version: "5.0.12.0", Addr: "127.0.0.1:6969", Answers: AppDS4Windows, Dir: true}},
			`{"t":1,"dsx":{"running":false,"addr":"","answers":""},"ds4windows":{"running":true,"version":"5.0.12.0","addr":"127.0.0.1:6969","answers":"ds4windows","dir":true},"auto":{"kind":"","why":"","sure":false}}`},
		{Item{ID: "dsx_profile", State: ItemWarn, Text: "x", How: "y", Fix: FixInstall},
			`{"id":"dsx_profile","state":"warn","text":"x","how":"y","fix":"profile.install"}`},
		{ProfileState{App: AppDS4Windows, Rev: 7, State: ProfileWaiting, Text: "x", Dir: `%APPDATA%\DS4Windows`,
			Steps: []ProfileStep{{ID: "listener", State: "todo", Text: "y"}}, Files: []string{"Profiles.xml"}, Items: []Item{}, CanCancel: true},
			`{"app":"ds4windows","rev":7,"state":"waiting","text":"x","dir":"%APPDATA%\\DS4Windows",` +
				`"steps":[{"id":"listener","state":"todo","text":"y"}],"files":["Profiles.xml"],"items":[],"backups":false,` +
				`"can_install":false,"can_reset":false,"can_cancel":true}`},
		{ProfileState{App: AppDSX, State: ProfileNotForElite, Player: "Mine", Steps: []ProfileStep{}, Files: []string{}, Items: []Item{},
			Backups: true, CanReset: true, Reset: &Confirm{Title: "t", Text: "x", OK: "Reset", Danger: true}},
			`{"app":"dsx","rev":0,"state":"not_for_elite","text":"","player_profile":"Mine","steps":[],"files":[],"items":[],"backups":true,` +
				`"can_install":false,"can_reset":true,"can_cancel":false,"reset":{"title":"t","text":"x","ok":"Reset","danger":true}}`},
		{Confirm{Title: "t", Text: "x", Items: []string{"a"}, Notes: []string{"n"}, OK: "Install"},
			`{"title":"t","text":"x","items":["a"],"notes":["n"],"ok":"Install"}`},
	} {
		b, err := json.Marshal(c.v)
		if err != nil || string(b) != c.want {
			t.Errorf("%T:\n got %s (%v)\nwant %s", c.v, b, err, c.want)
		}
	}
}

func TestLinksAreHTTPS(t *testing.T) {
	for id, u := range urls {
		if !strings.HasPrefix(u, "https://") {
			t.Errorf("link %s is %q", id, u)
		}
	}
}

func TestUIStateValid(t *testing.T) {
	ok := UIState{X: -1900, Y: 10, Width: 1040, Height: 720, Maximized: true, Page: "about"}
	if ok.Valid() != ok {
		t.Errorf("%+v was not kept", ok)
	}
	if got := (UIState{X: 1 << 20, Width: 800, Height: 600, Page: "home"}).Valid(); got != (UIState{Page: "home"}) {
		t.Errorf("far away placement: %+v", got)
	}
	if got := (UIState{Width: 800, Height: 600, Page: "nope", Theme: "dark"}).Valid(); got != (UIState{Width: 800, Height: 600, Theme: "dark"}) {
		t.Errorf("bad page: %+v", got)
	}
	if got := (UIState{Width: 800, Height: 600, Page: "home", Theme: "blue"}).Valid(); got != (UIState{Width: 800, Height: 600, Page: "home"}) {
		t.Errorf("bad theme: %+v", got)
	}
	if got := (UIState{Width: 20, Height: 600, Theme: "light"}).Valid(); got != (UIState{Theme: "light"}) {
		t.Errorf("too small: %+v", got)
	}
	if _, err := Check("ui.state", json.RawMessage(`{"theme":"blue"}`), false); err == nil {
		t.Error("ui.state took a theme that does not exist")
	}
}

func TestParse(t *testing.T) {
	l, err := Parse([]byte(`{"id":3,"m":"status.get"}`))
	if err != nil || l.M != "status.get" || l.ID != 3 {
		t.Errorf("request: %+v, %v", l, err)
	}
	l, err = Parse([]byte(`{"ev":"status","d":{"text":"x"}}`))
	if err != nil || l.Ev != "status" || string(l.D) != `{"text":"x"}` {
		t.Errorf("event: %+v, %v", l, err)
	}
	if _, err := Parse([]byte(`{"m":"a","ev":"b"}`)); err == nil {
		t.Error("a request and an event in one line was taken")
	}
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Error("garbage was taken")
	}
}

func TestReadLineCap(t *testing.T) {
	var got []Line
	long := `{"m":"x","p":"` + strings.Repeat("a", MaxLine) + `"}`
	in := `{"id":1,"m":"status.get"}` + "\n\n" + long + "\n" + `{"id":2,"m":"status.get"}` + "\n"
	err := Read(strings.NewReader(in), func(l Line) { got = append(got, l) })
	if err == nil {
		t.Fatal("a line over the cap was read")
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("read %+v before the long line", got)
	}
	// just under the cap is fine
	ok := `{"m":"x","p":"` + strings.Repeat("a", MaxLine-30) + `"}`
	got = nil
	if err := Read(strings.NewReader(ok+"\n"), func(l Line) { got = append(got, l) }); err != nil || len(got) != 1 {
		t.Errorf("a line under the cap: %v, %d lines", err, len(got))
	}
	if err := Read(strings.NewReader("[1,2]\n"), func(Line) {}); err == nil {
		t.Error("a line that is no message was read")
	}
}

// stuck is a writer that never returns until released: the other side
// stopped reading.
type stuck struct {
	release chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func (s *stuck) Write(b []byte) (int, error) {
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(b)
}

func TestSendNeverBlocks(t *testing.T) {
	w := &stuck{release: make(chan struct{})}
	var overflows atomic.Int32
	c := NewConn(w, func() { overflows.Add(1) })
	defer c.Close()
	done := make(chan struct{})
	var dropped, refused int
	go func() {
		defer close(done)
		for i := 0; i < 10*QueueSize; i++ {
			if !c.Send(Event{Ev: "status", D: i}, true) {
				dropped++
			}
		}
		// a message that may not be dropped overflows the full queue
		if !c.Send(Event{Ev: "notice", D: "x"}, false) {
			refused++
		}
		c.Send(Event{Ev: "notice", D: "y"}, false)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked")
	}
	if dropped < 9*QueueSize-1 {
		t.Errorf("only %d status events dropped", dropped)
	}
	if refused != 1 || overflows.Load() != 1 {
		t.Errorf("refused %d, overflow called %d times, want 1 and 1", refused, overflows.Load())
	}
	close(w.release)
	if !c.Flush(5 * time.Second) {
		t.Error("the queue was not written out")
	}
}

func TestSendAfterClose(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(&buf, nil)
	c.Close()
	if c.Send(Event{Ev: "bye"}, false) {
		t.Error("sent after Close")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestWriteErrorCloses(t *testing.T) {
	c := NewConn(failWriter{}, nil)
	c.Send(Event{Ev: "bye"}, false)
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a failed write did not close the Conn")
	}
}

// pipePair is two connected ends, as the core and the window see them.
func pipePair(t *testing.T, serve func(Line, *Conn)) *Client {
	t.Helper()
	toCore, fromWindow := io.Pipe()
	toWindow, fromCore := io.Pipe()
	core := NewConn(fromCore, nil)
	win := NewConn(fromWindow, nil)
	client := NewClient(win)
	go func() { _ = Read(toCore, func(l Line) { serve(l, core) }) }()
	go func() {
		_ = Read(toWindow, func(l Line) { client.Deliver(l) })
		client.Close()
	}()
	t.Cleanup(func() {
		core.Close()
		win.Close()
		fromCore.Close()
		fromWindow.Close()
	})
	return client
}

func TestClientCall(t *testing.T) {
	client := pipePair(t, func(l Line, c *Conn) {
		switch l.M {
		case "status.get":
			c.Send(Reply{ID: l.ID, OK: Status{Text: "Paused", Paused: true}}, false)
		default:
			c.Send(Reply{ID: l.ID, Err: &Error{Code: CodeUnknown, Msg: l.M}}, false)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := client.Call(ctx, "status.get", nil)
	if err != nil {
		t.Fatal(err)
	}
	var st Status
	if err := json.Unmarshal(raw, &st); err != nil || !st.Paused || st.Text != "Paused" {
		t.Errorf("status %s: %+v %v", raw, st, err)
	}
	_, err = client.Call(ctx, "nope", nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeUnknown {
		t.Errorf("unknown method: %v", err)
	}
}

func TestClientGone(t *testing.T) {
	client := pipePair(t, func(Line, *Conn) {}) // never answers
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := client.Call(ctx, "status.get", nil)
		errs <- err
	}()
	time.Sleep(50 * time.Millisecond)
	client.Close()
	var e *Error
	if err := <-errs; !errors.As(err, &e) || e.Code != CodeGone {
		t.Errorf("call after the core went: %v", err)
	}
}

func TestPacer(t *testing.T) {
	p := Pacer{Every: 250 * time.Millisecond}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if p.Wait(t0) != 0 {
		t.Error("the first send waited")
	}
	p.Sent(t0)
	if d := p.Wait(t0.Add(100 * time.Millisecond)); d != 150*time.Millisecond {
		t.Errorf("wait %v, want 150ms", d)
	}
	if d := p.Wait(t0.Add(300 * time.Millisecond)); d != 0 {
		t.Errorf("wait %v after the interval", d)
	}
}

func TestPumpRate(t *testing.T) {
	stop := make(chan struct{})
	wake := make(chan struct{}, 1)
	var sends atomic.Int32
	done := make(chan struct{})
	go func() {
		Pump(stop, wake, 50*time.Millisecond, func() bool { sends.Add(1); return true })
		close(done)
	}()
	// a burst of changes much faster than the pace
	end := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(end) {
		select {
		case wake <- struct{}{}:
		default:
		}
		time.Sleep(time.Millisecond)
	}
	close(stop)
	<-done
	n := sends.Load()
	// 500 ms at one send per 50 ms, plus the first one at once
	if n < 3 || n > 12 {
		t.Errorf("%d sends in 500 ms, want about 10", n)
	}
}

func TestPumpRetriesDropped(t *testing.T) {
	stop := make(chan struct{})
	wake := make(chan struct{}, 1)
	var tries atomic.Int32
	sent := make(chan struct{})
	go Pump(stop, wake, 10*time.Millisecond, func() bool {
		if tries.Add(1) < 3 {
			return false // queue full
		}
		close(sent)
		return true
	})
	defer close(stop)
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatalf("a dropped status was not tried again (%d tries)", tries.Load())
	}
}
