package window

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/tolgahan/ed-sense/internal/control"
)

type fakeCore struct {
	calls []string
	reply json.RawMessage
}

func (f *fakeCore) Call(_ context.Context, m string, p any) (json.RawMessage, error) {
	b, _ := json.Marshal(p)
	f.calls = append(f.calls, m+" "+string(b))
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
	// refused in the window, never reaching the core
	for _, r := range []Request{
		{M: "hello", P: json.RawMessage(`{"proto":1}`)},
		{M: "ui.state", P: json.RawMessage(`{"page":"home"}`)},
		{M: "ui.ready"},
		{M: "url.open", P: json.RawMessage(`{"id":"https://example.com"}`)},
		{M: "file.open", P: json.RawMessage(`{"which":"C:\\x"}`)},
		{M: "settings.patch", P: json.RawMessage(`{}`)},
	} {
		if _, err := b.Call(ctx, r); !errors.Is(err, errRefused) {
			t.Errorf("%s %s: %v", r.M, r.P, err)
		}
	}
	want := []string{`status.get null`, `url.open {"id":"source"}`}
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
