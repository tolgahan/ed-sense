package window

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/control"
)

// Request is one call from the page: a method and its params.
type Request struct {
	M string          `json:"m"`
	P json.RawMessage `json:"p,omitempty"`
}

// Caller reaches the core.
type Caller interface {
	Call(ctx context.Context, m string, p any) (json.RawMessage, error)
}

// callTimeout: the core answers at once, or it is gone.
const callTimeout = 10 * time.Second

// slowTimeout is for a switch of the controller app: it waits for another
// one and for a gyro calibration before the core answers.
const slowTimeout = 30 * time.Second

// timeout is how long the window waits for the core's answer to m.
func timeout(m string) time.Duration {
	switch m {
	case "engine.apply", "backend.choose":
		return slowTimeout
	}
	return callTimeout
}

// toPage: the window passes the core's event ev on to the page. It handles
// bye and focus itself.
func toPage(ev string) bool {
	return control.KnownEvent(ev) && ev != "bye" && ev != "focus"
}

// Bridge is the only Go the page can call. What the window handles
// itself is here; the rest must be in the allowlist, and goes to the core.
// Wails lets the page call every exported method, so Call is the only one.
type Bridge struct {
	Core      Caller
	Version   string
	ProtoOK   bool         // the core speaks this window's protocol
	After     int64        // the last notice before the core started this window
	Ready     func()       // the page has built its first view
	Front     func()       // before the core opens a file, a folder or a link: they may come to the front
	About     func() About // the signature, read when asked
	TextScale func() int   // Windows' Text size in percent; nil: 100

	// Appearance: the page picked a theme; show it and save it
	Appearance func(theme string)

	// The title bar's buttons, which the page draws.
	Minimise, ToggleMaximise, CloseWindow func()

	mu    sync.Mutex
	page  string
	theme string
	inits int   // win.init calls: more than one when the page was loaded again
	up    bool  // the page said it is ready
	seen  int64 // the newest notice the page has had
}

// Init is win.init's answer.
type Init struct {
	Version   string `json:"version"`
	ProtoOK   bool   `json:"proto_ok"`
	Page      string `json:"page"`
	Theme     string `json:"theme"`      // one of control.Themes
	TextScale int    `json:"text_scale"` // percent
	After     int64  `json:"after"`      // the messages after this notice are new to the player
}

// About is win.about's answer.
type About struct {
	Version    string `json:"version"`
	Signature  string `json:"signature"` // SignedValid, SignedInvalid or NotSigned
	Thumbprint string `json:"thumbprint"`
}

// Signature states.
const (
	SignedValid   = "valid"
	SignedInvalid = "invalid"
	NotSigned     = "none"
)

type pageParam struct {
	Page string `json:"page"`
}

type themeParam struct {
	Theme string `json:"theme"`
}

type readyParam struct {
	After int64 `json:"after"` // the newest notice the page has
}

// setPage is the page the window shows, for ui.state.
func (b *Bridge) setPage(p string) {
	b.mu.Lock()
	b.page = p
	b.mu.Unlock()
}

// lastPage is the page shown last.
func (b *Bridge) lastPage() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.page
}

// sawNotice: the core sent the page notice id, which it shows once it is
// up.
func (b *Bridge) sawNotice(id int64) {
	b.mu.Lock()
	if b.up {
		b.seen = max(b.seen, id)
	}
	b.mu.Unlock()
}

// after is win.init's After: the page shows the messages after it. A
// page loaded again (WebView2 does that after a crash) has had the ones
// before seen already.
func (b *Bridge) after() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inits++; b.inits == 1 {
		return b.After
	}
	return max(b.After, b.seen)
}

// setTheme is the theme the window shows; anything unknown is System.
func (b *Bridge) setTheme(t string) {
	if !slices.Contains(control.Themes, t) {
		t = control.ThemeSystem
	}
	b.mu.Lock()
	b.theme = t
	b.mu.Unlock()
}

// lastTheme is the theme picked last.
func (b *Bridge) lastTheme() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.theme == "" {
		return control.ThemeSystem
	}
	return b.theme
}

func (b *Bridge) textScale() int {
	if b.TextScale == nil {
		return 100
	}
	return b.TextScale()
}

// errRefused hides from the page why a call was refused; the log has it.
var errRefused = errors.New("not allowed")

// Call answers the page.
func (b *Bridge) Call(ctx context.Context, req Request) (any, error) {
	switch req.M {
	case "win.init":
		return Init{Version: b.Version, ProtoOK: b.ProtoOK, Page: b.lastPage(), Theme: b.lastTheme(), TextScale: b.textScale(), After: b.after()}, nil
	case "win.ready":
		var p readyParam
		_ = json.Unmarshal(req.P, &p) // optional
		b.mu.Lock()
		b.up = true
		b.seen = max(b.seen, p.After)
		b.mu.Unlock()
		if b.Ready != nil {
			b.Ready()
		}
		return nil, nil
	case "win.page":
		var p pageParam
		if json.Unmarshal(req.P, &p) != nil || !slices.Contains(control.Pages, p.Page) {
			return nil, errRefused
		}
		b.setPage(p.Page)
		return nil, nil
	case "win.min", "win.max", "win.close":
		f := map[string]func(){"win.min": b.Minimise, "win.max": b.ToggleMaximise, "win.close": b.CloseWindow}[req.M]
		if f != nil {
			f()
		}
		return nil, nil
	case "win.theme":
		var p themeParam
		if json.Unmarshal(req.P, &p) != nil || !slices.Contains(control.Themes, p.Theme) {
			return nil, errRefused
		}
		b.setTheme(p.Theme)
		if b.Appearance != nil {
			b.Appearance(p.Theme)
		}
		return nil, nil
	case "win.about":
		if b.About == nil {
			return About{Version: b.Version, Signature: NotSigned}, nil
		}
		a := b.About()
		a.Version = b.Version
		return a, nil
	}
	if _, err := control.Check(req.M, req.P, true); err != nil {
		return nil, errRefused
	}
	if (req.M == "url.open" || req.M == "file.open" || req.M == "folder.open") && b.Front != nil {
		b.Front()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout(req.M))
	defer cancel()
	var p any
	if len(req.P) > 0 {
		p = req.P
	}
	raw, err := b.Core.Call(ctx, req.M, p)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	return raw, nil
}
