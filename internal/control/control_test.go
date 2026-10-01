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
		{"hello", `{"proto":1}`, false, ""},
		{"hello", `{"proto":1}`, true, CodeUnknown}, // the window process's own
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
		{"settings.patch", `{}`, true, CodeUnknown}, // not in W1
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
