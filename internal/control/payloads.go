package control

// Settings is settings.get's answer and the "config" event: the settings
// file as the core's Store last saw it.
type Settings struct {
	Rev      int64   `json:"rev"`    // per run of the core, from 1; a newer one is larger
	Config   any     `json:"config"` // the settings, as edsense.json holds them; the last good ones while it is broken
	Broken   *Broken `json:"broken"` // the file has an error; null when it has none
	FirstRun bool    `json:"first_run"`
}

// Broken is where the settings file has an error. Line and Col are 0 when
// the file could not be read at all.
type Broken struct {
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Key  string `json:"key"` // the setting with a wrong type, e.g. "triggers.hit.params.1"; "" for others
	Msg  string `json:"msg"`
}

// Patched is settings.patch's answer. A patch with problems changes
// nothing.
type Patched struct {
	Applied  bool      `json:"applied"` // written, or there was nothing to change
	Rev      int64     `json:"rev"`     // the Settings' rev after it
	Problems []Problem `json:"problems"`
}

// Problem is why a patch was refused: path is the setting ("" for the
// patch itself), code one of the settings' problem codes.
type Problem struct {
	Path string `json:"path"`
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

// Engine is what drives the controller now: engine.apply's and
// backend.choose's answer.
type Engine struct {
	Choice    string   `json:"choice"` // AppAuto, AppDSX or AppDS4Windows
	Pinned    bool     `json:"pinned"` // -backend decides this run
	Kind      string   `json:"kind"`   // AppDSX or AppDS4Windows
	Name      string   `json:"name"`
	Why       string   `json:"why"`
	Addr      string   `json:"addr"`
	Switching bool     `json:"switching"`
	Pending   []string `json:"pending"` // settings changed in the file that apply with Apply now
	// NotSaved: backend.choose switched, but the choice could not be
	// saved, so it lasts until EDSense quits; why. "" when it was saved.
	NotSaved string `json:"not_saved,omitempty"`
}

// Detection is backend.detect's answer: which controller apps run, and
// who answers on their ports.
type Detection struct {
	T          int64    `json:"t"` // Unix milliseconds
	DSX        Seen     `json:"dsx"`
	DS4Windows Seen     `json:"ds4windows"`
	Auto       AutoPick `json:"auto"`
}

// Seen is one controller app as a detection saw it.
type Seen struct {
	Running bool   `json:"running"`
	Version string `json:"version,omitempty"` // DS4Windows' version, when it runs
	Addr    string `json:"addr"`              // where EDSense sends to it
	Answers string `json:"answers"`           // who answers there: AppDSX, AppDS4Windows or ""
	Dir     bool   `json:"dir,omitempty"`     // DS4Windows: its settings were found
}

// AutoPick is what Auto would pick now. Sure: the app runs, or answered.
type AutoPick struct {
	Kind string `json:"kind"`
	Why  string `json:"why"`
	Sure bool   `json:"sure"`
}

// Setup is setup.check's answer: the checklist of one controller app.
type Setup struct {
	App   string `json:"app"`
	T     int64  `json:"t"` // Unix milliseconds
	Items []Item `json:"items"`
}

// Item is one row of a setup checklist.
type Item struct {
	ID    string `json:"id"`
	State string `json:"state"` // one of the Item states
	Text  string `json:"text"`
	How   string `json:"how,omitempty"`  // what to do about it
	Link  string `json:"link,omitempty"` // a url.open id that helps
}

// Item states.
const (
	ItemOK      = "ok"
	ItemWarn    = "warn"    // EDSense works, with less
	ItemBad     = "bad"     // it keeps EDSense from working
	ItemWait    = "wait"    // checked once something else is there
	ItemUnknown = "unknown" // it could not be checked
	ItemLater   = "later"   // checked once EDSense uses the app
)

// ProfileReset is profile.reset's answer.
type ProfileReset struct {
	App   string `json:"app"`
	State string `json:"state"` // ResetWaiting or ResetUnavailable
	Text  string `json:"text"`
}

// Profile reset states.
const (
	ResetWaiting     = "waiting"     // asked for: done as soon as the app is closed
	ResetUnavailable = "unavailable" // not while EDSense uses another app
)
