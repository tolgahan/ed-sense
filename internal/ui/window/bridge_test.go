package window

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/control"
)

type fakeCore struct {
	calls []string
	reply json.RawMessage
	waits map[string]time.Duration // how long each method may take, by its context
}

func (f *fakeCore) Call(ctx context.Context, m string, p any) (json.RawMessage, error) {
	b, _ := json.Marshal(p)
	f.calls = append(f.calls, m+" "+string(b))
	if end, ok := ctx.Deadline(); ok {
		if f.waits == nil {
			f.waits = map[string]time.Duration{}
		}
		f.waits[m] = time.Until(end)
	}
	if m == "demo.play" {
		return nil, &control.Error{Code: control.CodeFailed, Msg: "busy"}
	}
	return f.reply, nil
}

func TestBridge(t *testing.T) {
	core := &fakeCore{reply: json.RawMessage(`{"text":"Paused"}`)}
	front := 0
	ready := 0
	var themes []string
	b := &Bridge{Core: core, Version: "1.2.3", ProtoOK: true, After: 42, Ready: func() { ready++ }, Front: func() { front++ },
		About:      func() About { return About{Signature: SignedValid, Thumbprint: "ABC"} },
		Appearance: func(t string) { themes = append(themes, t) }}
	ctx := context.Background()

	v, err := b.Call(ctx, Request{M: "win.init"})
	if err != nil || v.(Init) != (Init{Version: "1.2.3", ProtoOK: true, Theme: "system", TextScale: 100, After: 42}) {
		t.Errorf("win.init = %+v, %v", v, err)
	}
	if _, err := b.Call(ctx, Request{M: "win.page", P: json.RawMessage(`{"page":"about"}`)}); err != nil || b.lastPage() != "about" {
		t.Errorf("win.page: %v, page %q", err, b.lastPage())
	}
	if _, err := b.Call(ctx, Request{M: "win.page", P: json.RawMessage(`{"page":"../x"}`)}); err == nil || b.lastPage() != "about" {
		t.Errorf("a bad page was taken: %v", err)
	}
	if _, err := b.Call(ctx, Request{M: "win.theme", P: json.RawMessage(`{"theme":"dark"}`)}); err != nil || b.lastTheme() != "dark" {
		t.Errorf("win.theme: %v, theme %q", err, b.lastTheme())
	}
	if _, err := b.Call(ctx, Request{M: "win.theme", P: json.RawMessage(`{"theme":"blue"}`)}); err == nil || b.lastTheme() != "dark" {
		t.Errorf("a bad theme was taken: %v", err)
	}
	if !reflect.DeepEqual(themes, []string{"dark"}) {
		t.Errorf("themes shown: %q", themes)
	}
	if v, _ := b.Call(ctx, Request{M: "win.init"}); v.(Init).Theme != "dark" {
		t.Errorf("win.init after win.theme = %+v", v)
	}
	b.sawNotice(30) // before the page is up: not seen
	if _, err := b.Call(ctx, Request{M: "win.ready", P: json.RawMessage(`{"after":50}`)}); err != nil || ready != 1 {
		t.Errorf("win.ready: %v, %d", err, ready)
	}
	if v, _ := b.Call(ctx, Request{M: "win.about"}); v.(About) != (About{Version: "1.2.3", Signature: SignedValid, Thumbprint: "ABC"}) {
		t.Errorf("win.about = %+v", v)
	}

	v, err = b.Call(ctx, Request{M: "status.get"})
	if err != nil || string(v.(json.RawMessage)) != `{"text":"Paused"}` {
		t.Errorf("status.get = %v, %v", v, err)
	}
	if _, err := b.Call(ctx, Request{M: "url.open", P: json.RawMessage(`{"id":"source"}`)}); err != nil || front != 1 {
		t.Errorf("url.open: %v, front %d", err, front)
	}
	if _, err := b.Call(ctx, Request{M: "folder.open", P: json.RawMessage(`{"which":"ds4windows_backups"}`)}); err != nil || front != 2 {
		t.Errorf("folder.open: %v, front %d", err, front)
	}
	// refused in the window, never reaching the core
	for _, r := range []Request{
		{M: "hello", P: json.RawMessage(`{"proto":1}`)},
		{M: "ui.state", P: json.RawMessage(`{"page":"home"}`)},
		{M: "ui.ready"},
		{M: "url.open", P: json.RawMessage(`{"id":"https://example.com"}`)},
		{M: "file.open", P: json.RawMessage(`{"which":"C:\\x"}`)},
		{M: "settings.patch", P: json.RawMessage(`{}`)},
		{M: "settings.patch", P: json.RawMessage(`{"patch":[1]}`)},
		{M: "backend.choose", P: json.RawMessage(`{"choice":"xbox"}`)},
		{M: "setup.check", P: json.RawMessage(`{"app":"auto"}`)},
		{M: "profile.reset", P: json.RawMessage(`{"app":"dsx"}`)},
		{M: "profile.install", P: json.RawMessage(`{"app":"auto"}`)},
		{M: "profile.state", P: json.RawMessage(`{"app":"dsx","dir":"x"}`)},
		{M: "profile.cancel"},
		{M: "folder.open", P: json.RawMessage(`{"which":"edsense"}`)},
		{M: "folder.open", P: json.RawMessage(`{"path":"C:/Windows"}`)},
	} {
		if _, err := b.Call(ctx, r); !errors.Is(err, errRefused) {
			t.Errorf("%s %s: %v", r.M, r.P, err)
		}
	}
	want := []string{`status.get null`, `url.open {"id":"source"}`, `folder.open {"which":"ds4windows_backups"}`}
	if len(core.calls) != len(want) {
		t.Fatalf("the core got %q", core.calls)
	}
	for i := range want {
		if core.calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, core.calls[i], want[i])
		}
	}
	var e *control.Error
	if _, err := b.Call(ctx, Request{M: "demo.play"}); !errors.As(err, &e) || e.Code != control.CodeFailed {
		t.Errorf("a failed call: %v", err)
	}

	var pressed []string
	b.Minimise = func() { pressed = append(pressed, "min") }
	b.ToggleMaximise = func() { pressed = append(pressed, "max") }
	b.CloseWindow = func() { pressed = append(pressed, "close") }
	for _, m := range []string{"win.min", "win.max", "win.close"} {
		if _, err := b.Call(ctx, Request{M: m}); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
	if !reflect.DeepEqual(pressed, []string{"min", "max", "close"}) {
		t.Errorf("the title bar's buttons did %q", pressed)
	}

	b.TextScale = func() int { return 150 }
	if v, _ := b.Call(ctx, Request{M: "win.init"}); v.(Init).TextScale != 150 {
		t.Errorf("win.init = %+v", v)
	}
}

// TestBridgeV2: the settings, engine and setup calls pass to the core as
// the page sent them; a switch may take longer than the others.
func TestBridgeV2(t *testing.T) {
	core := &fakeCore{reply: json.RawMessage(`{"rev":1}`)}
	b := &Bridge{Core: core, ProtoOK: true}
	ctx := context.Background()
	reqs := []Request{
		{M: "settings.get"},
		{M: "settings.schema"},
		{M: "settings.patch", P: json.RawMessage(`{"patch":{"ds4windows_port":0}}`)},
		{M: "engine.apply"},
		{M: "backend.choose", P: json.RawMessage(`{"choice":"ds4windows"}`)},
		{M: "backend.detect", P: json.RawMessage(`{"fresh":true}`)},
		{M: "setup.check", P: json.RawMessage(`{"app":"ds4windows","fresh":false}`)},
		{M: "profile.state", P: json.RawMessage(`{"app":"ds4windows"}`)},
		{M: "profile.install", P: json.RawMessage(`{"app":"dsx","reset":true}`)},
		{M: "profile.cancel", P: json.RawMessage(`{"app":"ds4windows"}`)},
	}
	var want []string
	for _, r := range reqs {
		if _, err := b.Call(ctx, r); err != nil {
			t.Errorf("%s: %v", r.M, err)
		}
		p := "null"
		if r.P != nil {
			p = string(r.P)
		}
		want = append(want, r.M+" "+p)
	}
	if !reflect.DeepEqual(core.calls, want) {
		t.Errorf("the core got\n%q\nwant\n%q", core.calls, want)
	}
	for m, d := range core.waits {
		limit := callTimeout
		if m == "engine.apply" || m == "backend.choose" {
			limit = slowTimeout
		}
		if d > limit || d < limit-5*time.Second {
			t.Errorf("%s may take %v, want %v", m, d, limit)
		}
	}
	if slowTimeout < 3*callTimeout/2 {
		t.Errorf("a switch may take only %v", slowTimeout)
	}
}

// TestToPage: the page gets every event the core may send, but bye and
// focus, which the window handles itself.
func TestToPage(t *testing.T) {
	for ev, want := range map[string]bool{"status": true, "notice": true, "config": true,
		"profile": true, "bye": false, "focus": false, "edsense:status": false, "": false} {
		if got := toPage(ev); got != want {
			t.Errorf("toPage(%q) = %v", ev, got)
		}
	}
}

// TestBridgeReload: a page loaded again shows only the messages it has
// not had.
func TestBridgeReload(t *testing.T) {
	b := &Bridge{Core: &fakeCore{}, ProtoOK: true, After: 10}
	ctx := context.Background()
	init := func() int64 {
		v, err := b.Call(ctx, Request{M: "win.init"})
		if err != nil {
			t.Fatal(err)
		}
		return v.(Init).After
	}
	b.sawNotice(20) // the page is not up yet: it may not have had it
	if got := init(); got != 10 {
		t.Errorf("first win.init After = %d, want 10", got)
	}
	if _, err := b.Call(ctx, Request{M: "win.ready", P: json.RawMessage(`{"after":15}`)}); err != nil {
		t.Fatal(err)
	}
	b.sawNotice(25)
	if got := init(); got != 25 {
		t.Errorf("win.init after a reload: After = %d, want 25", got)
	}
}

// TestBridgeMethods: Wails binds every exported method for the page.
func TestBridgeMethods(t *testing.T) {
	ty := reflect.TypeOf(&Bridge{})
	if ty.NumMethod() != 1 || ty.Method(0).Name != "Call" {
		var names []string
		for i := 0; i < ty.NumMethod(); i++ {
			names = append(names, ty.Method(i).Name)
		}
		t.Errorf("Bridge's exported methods: %v, want only Call", names)
	}
}
