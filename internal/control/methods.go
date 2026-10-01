package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// method is one entry of the allowlist.
type method struct {
	page   bool                               // the page may call it; else only the window process itself
	params func(json.RawMessage) (any, error) // nil: it takes none
}

// methods is everything the window may ask of the core.
var methods = map[string]method{
	"hello":           {params: decode[Hello]},
	"status.get":      {page: true},
	"status.watch":    {page: true, params: decode[Watch]},
	"notices.list":    {page: true},
	"pause.set":       {page: true, params: decode[Pause]},
	"demo.play":       {page: true},
	"demo.stop":       {page: true},
	"gyro.calibrate":  {page: true},
	"file.open":       {page: true, params: checked(decode[File], File.check)},
	"url.open":        {page: true, params: checked(decode[URL], URL.check)},
	"app.quit":        {page: true},
	"settings.get":    {page: true},
	"settings.schema": {page: true},
	"settings.patch":  {page: true, params: checked(decode[Patch], Patch.check)},
	"engine.apply":    {page: true},
	"backend.choose":  {page: true, params: checked(decode[Choice], Choice.check)},
	"backend.detect":  {page: true, params: decode[Detect]},
	"setup.check":     {page: true, params: checked(decode[SetupCheck], SetupCheck.check)},
	"profile.reset":   {page: true, params: checked(decode[Profile], Profile.check)},
	"ui.state":        {params: checked(decode[UIState], UIState.check)},
	"ui.ready":        {}, // the page is up: WebView2 started
	"ui.closing":      {}, // the window is going: it shows nothing more
}

// events is everything the core may send the window.
var events = map[string]bool{
	"status": true, // coalesced, at most 4 a second
	"notice": true,
	"config": true, // the settings file changed: Settings
	"focus":  true, // come to the front
	"bye":    true, // the core quits
}

// Check parses a request's params when m is in the allowlist. fromPage:
// the request comes from the page, which may call fewer methods than the
// window process.
func Check(m string, p json.RawMessage, fromPage bool) (any, *Error) {
	spec, ok := methods[m]
	if !ok || fromPage && !spec.page {
		return nil, &Error{Code: CodeUnknown, Msg: fmt.Sprintf("%q is not allowed", m)}
	}
	if spec.params == nil {
		if len(bytes.TrimSpace(p)) != 0 && !bytes.Equal(bytes.TrimSpace(p), []byte("null")) && !bytes.Equal(bytes.TrimSpace(p), []byte("{}")) {
			return nil, &Error{Code: CodeParams, Msg: m + " takes no parameters"}
		}
		return nil, nil
	}
	v, err := spec.params(p)
	if err != nil {
		return nil, &Error{Code: CodeParams, Msg: m + ": " + err.Error()}
	}
	return v, nil
}

// PageMethod reports whether the page may call m.
func PageMethod(m string) bool { return methods[m].page }

// KnownEvent reports whether the core may send ev.
func KnownEvent(ev string) bool { return events[ev] }

// decode reads params strictly: unknown fields are refused.
func decode[T any](p json.RawMessage) (any, error) {
	var v T
	if len(bytes.TrimSpace(p)) == 0 {
		return v, nil
	}
	d := json.NewDecoder(bytes.NewReader(p))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, fmt.Errorf("more than one value")
	}
	return v, nil
}

func checked[T any](parse func(json.RawMessage) (any, error), check func(T) error) func(json.RawMessage) (any, error) {
	return func(p json.RawMessage) (any, error) {
		v, err := parse(p)
		if err != nil {
			return nil, err
		}
		if err := check(v.(T)); err != nil {
			return nil, err
		}
		return v, nil
	}
}

// Hello opens the conversation, from the window process.
type Hello struct {
	Proto int `json:"proto"`
}

// Welcome answers Hello.
type Welcome struct {
	Proto   int     `json:"proto"`
	Version string  `json:"version"`
	Page    string  `json:"page"`
	Place   UIState `json:"place"`
	After   int64   `json:"after"` // the last notice before this window started: it shows the messages after it
}

// Watch starts or stops the status events (the page stops them while it
// cannot be seen).
type Watch struct {
	On bool `json:"on"`
}

// Pause is pause.set's parameter.
type Pause struct {
	Paused bool `json:"paused"`
}

// Files the page may open, by name.
const (
	FileSettings = "settings"
	FileLog      = "log"
)

// File is file.open's parameter.
type File struct {
	Which string `json:"which"`
}

func (f File) check() error {
	if f.Which != FileSettings && f.Which != FileLog {
		return fmt.Errorf("no file %q", f.Which)
	}
	return nil
}

// MaxPatch is the longest settings patch.
const MaxPatch = 64 << 10

// Patch is settings.patch's parameter: a JSON merge patch of the settings
// (RFC 7396), one object.
type Patch struct {
	Patch json.RawMessage `json:"patch"`
}

func (p Patch) check() error {
	b := bytes.TrimSpace(p.Patch)
	if len(b) == 0 || b[0] != '{' {
		return fmt.Errorf("the patch is not an object")
	}
	// measured as the core reads it: the window sends it on compacted, with
	// <, >, & and U+2028/U+2029 escaped, so both sides count the same bytes
	if sent, err := json.Marshal(p.Patch); err != nil || len(sent) > MaxPatch {
		return fmt.Errorf("the patch is longer than %d KiB", MaxPatch>>10)
	}
	return nil
}

// The controller apps, as the settings file names them.
const (
	AppAuto       = "auto"
	AppDSX        = "dsx"
	AppDS4Windows = "ds4windows"
)

// Choice is backend.choose's parameter. KeepPin: the first run's "Set up
// later", which only saves the choice while -backend decides this run.
type Choice struct {
	Choice  string `json:"choice"`
	KeepPin bool   `json:"keep_pin,omitempty"`
}

func (c Choice) check() error {
	switch c.Choice {
	case AppAuto, AppDSX, AppDS4Windows:
		return nil
	}
	return fmt.Errorf("no controller app %q", c.Choice)
}

// Detect is backend.detect's parameter. Fresh: look again, though a look
// from the last 2 seconds is there.
type Detect struct {
	Fresh bool `json:"fresh"`
}

// SetupCheck is setup.check's parameter: the app whose setup is checked.
type SetupCheck struct {
	App   string `json:"app"`
	Fresh bool   `json:"fresh"`
}

func (s SetupCheck) check() error {
	if s.App != AppDSX && s.App != AppDS4Windows {
		return fmt.Errorf("no controller app %q", s.App)
	}
	return nil
}

// Profile is profile.reset's parameter: the app whose profile for Elite
// EDSense puts back. Only DSX's can be reset.
type Profile struct {
	App string `json:"app"`
}

func (p Profile) check() error {
	if p.App != AppDSX {
		return fmt.Errorf("no profile reset for %q", p.App)
	}
	return nil
}

// URL is url.open's parameter: an id from the table, never an address.
type URL struct {
	ID string `json:"id"`
}

func (u URL) check() error {
	if _, ok := urls[u.ID]; !ok {
		return fmt.Errorf("no link %q", u.ID)
	}
	return nil
}

// urls are the only addresses the window can open.
var urls = map[string]string{
	"source":    "https://github.com/tolgahan/ed-sense",
	"releases":  "https://github.com/tolgahan/ed-sense/releases",
	"license":   "https://github.com/tolgahan/ed-sense/blob/main/LICENSE",
	"notices":   "https://github.com/tolgahan/ed-sense/blob/main/THIRD_PARTY_NOTICES.txt",
	"signature": "https://github.com/tolgahan/ed-sense/blob/main/docs/how-it-works.md#checking-a-download",
	"gmh":       "https://github.com/JibbSmart/GamepadMotionHelpers",
	"jsm":       "https://github.com/Electronicks/JoyShockMapper",
	"wails":     "https://github.com/wailsapp/wails",
	"webview2":  WebView2Download,

	"ds4windows_doc":      "https://github.com/tolgahan/ed-sense/blob/main/docs/ds4windows.md",
	"ds4windows_releases": "https://github.com/hbashton/DS4Windows/releases",
}

// WebView2Download is Microsoft's page for the WebView2 Runtime.
const WebView2Download = "https://developer.microsoft.com/microsoft-edge/webview2/consumer/"

// Address is the address behind a link id.
func Address(id string) (string, bool) {
	u, ok := urls[id]
	return u, ok
}

// Pages are the window's pages, by id.
var Pages = []string{"home", "controller", "feel", "triggers", "lights", "gyro", "hud", "advanced", "about"}

// The window's appearances: System follows Windows' light or dark mode.
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// Themes are the appearances by id; "" is ThemeSystem.
var Themes = []string{ThemeSystem, ThemeLight, ThemeDark}

// UIState is the window's placement in physical pixels, the page shown
// and the appearance. Zero size: none saved.
type UIState struct {
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Maximized bool   `json:"maximized"`
	Page      string `json:"page"`
	Theme     string `json:"theme,omitempty"`
}

// Placement limits, generous for any monitor layout.
const (
	maxCoord = 1 << 16
	minSize  = 100
)

func (s UIState) check() error {
	if s.Page != "" && !slices.Contains(Pages, s.Page) {
		return fmt.Errorf("no page %q", s.Page)
	}
	if s.Theme != "" && !slices.Contains(Themes, s.Theme) {
		return fmt.Errorf("no theme %q", s.Theme)
	}
	return s.checkPlace()
}

func (s UIState) checkPlace() error {
	if s.Width == 0 && s.Height == 0 {
		return nil
	}
	if s.Width < minSize || s.Height < minSize || s.Width > maxCoord || s.Height > maxCoord ||
		s.X < -maxCoord || s.X > maxCoord || s.Y < -maxCoord || s.Y > maxCoord {
		return fmt.Errorf("placement out of range")
	}
	return nil
}

// Valid is s with each part that fails its check left out: the
// placement, the page, the theme.
func (s UIState) Valid() UIState {
	v := s
	if s.checkPlace() != nil {
		v = UIState{Page: s.Page, Theme: s.Theme}
	}
	if !slices.Contains(Pages, v.Page) {
		v.Page = ""
	}
	if !slices.Contains(Themes, v.Theme) {
		v.Theme = ""
	}
	return v
}
