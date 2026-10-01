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
	Fix   string `json:"fix,omitempty"`  // FixInstall: the app's profile card can fix it
}

// FixInstall is an Item's Fix: installing EDSense's profile for the app
// (profile.install) fixes it.
const FixInstall = "profile.install"

// Item states.
const (
	ItemOK      = "ok"
	ItemWarn    = "warn"    // EDSense works, with less
	ItemBad     = "bad"     // it keeps EDSense from working
	ItemWait    = "wait"    // checked once something else is there
	ItemUnknown = "unknown" // it could not be checked
	ItemLater   = "later"   // checked once EDSense uses the app
)

// ProfileState is an app's profile card: what EDSense finds of its own
// profile for Elite in that app, what it can do about it, and the job
// under way. It is the answer of profile.state, profile.install and
// profile.cancel, and the "profile" event.
type ProfileState struct {
	App   string `json:"app"`             // AppDSX or AppDS4Windows
	Rev   int64  `json:"rev"`             // per run of the core, over both apps; a newer one is larger
	State string `json:"state"`           // one of the Profile states
	Block string `json:"block,omitempty"` // ProfileBlocked: why, one of the Block ids
	Text  string `json:"text"`
	// Player is the player's own profile the text names, for Elite or
	// the controller's usual one; "" when it names none.
	Player  string        `json:"player_profile,omitempty"`
	Dir     string        `json:"dir,omitempty"` // the app's folder, as the player knows it: "%APPDATA%\DS4Windows"
	Steps   []ProfileStep `json:"steps"`         // DS4Windows: profile, rule and listener
	Files   []string      `json:"files"`         // what the action offered writes, in Dir
	Items   []Item        `json:"items"`         // DS4Windows, ProfileOther: the checks of the player's profile
	Backups bool          `json:"backups"`       // the copies' folder is there, for folder.open
	Link    string        `json:"link,omitempty"`
	// What the card offers: profile.install, with reset, and
	// profile.cancel.
	CanInstall bool `json:"can_install"`
	CanReset   bool `json:"can_reset"`
	CanCancel  bool `json:"can_cancel"`
	// The questions asked before profile.install, and before it with
	// reset; nil when it is not offered.
	Install *Confirm `json:"install,omitempty"`
	Reset   *Confirm `json:"reset,omitempty"`
}

// ProfileStep is one part of a DS4Windows install.
type ProfileStep struct {
	ID    string `json:"id"`    // "profile", "rule" or "listener"
	State string `json:"state"` // "todo", "done" or "skip"
	Text  string `json:"text"`
}

// Confirm is a question the page asks before an action: the text, a list
// of what it does, then the notes. Key names what it asks; the page sends
// it with the action, which is refused when the question would be another
// one by then.
type Confirm struct {
	Title  string   `json:"title"`
	Text   string   `json:"text"`
	Items  []string `json:"items,omitempty"`
	Notes  []string `json:"notes,omitempty"`
	OK     string   `json:"ok"`
	Danger bool     `json:"danger,omitempty"`
	Key    string   `json:"key,omitempty"`
}

// Profile states. DSX has unknown, no_folder, missing, present,
// not_for_elite, waiting, writing, done and failed; DS4Windows has
// unknown, blocked, missing, partial, ours, other, usual_ok, waiting,
// writing, done, failed and interrupted.
const (
	ProfileUnknown     = "unknown"       // not looked at yet
	ProfileNoFolder    = "no_folder"     // DSX's folder was not found
	ProfileBlocked     = "blocked"       // EDSense leaves DS4Windows' files alone: Block says why
	ProfileMissing     = "missing"       // EDSense's profile is not there
	ProfilePartial     = "partial"       // some of EDSense's DS4Windows setup is there
	ProfilePresent     = "present"       // DSX has an "Elite Dangerous" profile, used for Elite
	ProfileNotForElite = "not_for_elite" // DSX has it, but Elite gets another profile, or none
	ProfileOurs        = "ours"          // Elite uses EDSense's DS4Windows profile
	ProfileOther       = "other"         // Elite has the player's own Auto Profiles rule
	ProfileUsualOK     = "usual_ok"      // the player's usual DS4Windows profile works already
	ProfileWaiting     = "waiting"       // a write waits for the app to be closed
	ProfileWriting     = "writing"       // the write runs: it can no longer be cancelled
	ProfileDone        = "done"          // written: it is used once the app starts again
	ProfileFailed      = "failed"        // not written
	ProfileInterrupted = "interrupted"   // DS4Windows started while EDSense wrote: some of it is written
)
