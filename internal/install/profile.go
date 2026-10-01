package install

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
)

// The folders the copies go in, in EDSense's data folder.
const (
	DSXBackups        = "dsx_profile_backups"
	DS4WindowsBackups = "ds4windows_backups"
)

// DSXResetQuestion asks before DSX's "Elite Dangerous" profile is put
// back: in the window, and in the tray while the window cannot open.
const DSXResetQuestion = "Replace DSX's \"" + dsx.ProfileName + "\" controller profile with the one that comes with EDSense?\n\n" +
	"Your current profile is kept in the " + DSXBackups + " folder next to EDSense.\n\n" +
	"DSX must be closed for this. If it is running, close it now (DSX tray icon > Exit): EDSense does the reset as soon as DSX is closed. Then start DSX again."

// DSXGameNote is what a reset changes in DSX's game profile for Elite,
// which names forElite now: "" when it names EDSense's profile.
func DSXGameNote(forElite string) string {
	switch forElite {
	case dsx.ProfileName:
		return ""
	case "":
		return "It also becomes Elite's game profile in DSX, which picks no profile for Elite now."
	}
	return fmt.Sprintf("Elite's game profile in DSX changes from %q to %q too. A copy of DSX's game profiles goes to %s.",
		forElite, dsx.ProfileName, DSXBackups)
}

// dsxSeen is what a look at DSX's files found.
type dsxSeen struct {
	look    dsx.Look
	backups bool   // the copies' folder is there
	shown   string // DSX's folder, as the player knows it
	autoAdd bool   // the first add may still add the profile by itself in this run
	at      time.Time
}

// key names what a question asks, so an answer is taken only for the
// question shown: the app, the folder, the action and what it writes.
func key(app, dir, action string, what ...string) string {
	return strings.Join(append([]string{app, dir, action}, what...), "|")
}

// dsxProfile is DSX's profile card: the job j over the files l.
func dsxProfile(l dsxSeen, j dsx.Job) control.ProfileState {
	st := emptyState(control.AppDSX)
	if l.at.IsZero() && j.State == "" {
		st.State, st.Text = control.ProfileUnknown, "Checking DSX's profiles..."
		return st
	}
	s := dsx.StateOf(l.look, j)
	st.Dir, st.Backups = l.shown, l.backups
	switch s.State {
	case dsx.StateNoFolder:
		st.State, st.Text = control.ProfileNoFolder, "DSX's folder was not found, so the profile could not be written."
	case dsx.StateMissing:
		st.State = control.ProfileMissing
		st.Text = "EDSense comes with a DSX profile for Elite Dangerous (gyro aim, touchpad and trigger setup). Install... adds it."
		if l.autoAdd {
			st.Text = "EDSense comes with a DSX profile for Elite Dangerous (gyro aim, touchpad and trigger setup). It is added when DSX is closed."
		}
	case dsx.StatePresent:
		st.State, st.Text = control.ProfilePresent, "DSX has an \""+dsx.ProfileName+"\" profile, set for Elite."
	case dsx.StateNotForElite:
		st.State, st.Player = control.ProfileNotForElite, s.Msg
		st.Text = "DSX has an \"" + dsx.ProfileName + "\" profile, but DSX does not pick it when Elite starts."
		if s.Msg != "" {
			st.Text = "DSX has an \"" + dsx.ProfileName + "\" profile, but Elite gets the profile \"" + s.Msg + "\"."
		}
	case dsx.StateWaiting:
		st.State, st.Text, st.CanCancel = control.ProfileWaiting, "Waiting for DSX to close (DSX tray icon > Exit).", true
		st.Files = slices.Clone(dsx.ProfileFiles)
		return st
	case dsx.StateWriting:
		st.State, st.Text = control.ProfileWriting, "Writing the DSX profile now."
		st.Files = slices.Clone(dsx.ProfileFiles)
		return st
	case dsx.StateDone:
		st.State = control.ProfileDone
		st.Text = fmt.Sprintf("The %q controller profile is now in DSX. Start DSX again to use it.", dsx.ProfileName)
		return st
	case dsx.StateFailed:
		st.State, st.Text = control.ProfileFailed, "The DSX profile could not be written: "+sentence(s.Msg)
	}
	if l.look.Folder == "" {
		return st
	}
	if l.look.Has {
		q := &control.Confirm{Title: "Reset the DSX profile?", Text: DSXResetQuestion, OK: "Reset", Danger: true,
			Key: key(control.AppDSX, l.shown, "reset", l.look.ForElite)}
		if note := DSXGameNote(l.look.ForElite); note != "" {
			q.Notes = []string{note}
		}
		st.CanReset, st.Reset = true, q
	} else {
		st.CanInstall = true
		st.Install = &control.Confirm{
			Title: "Add the DSX profile for Elite?",
			Text: "EDSense adds its DSX controller profile \"" + dsx.ProfileName + "\" (gyro aim, touchpad and trigger setup), " +
				"and has DSX use it for Elite when Elite has no game profile in DSX yet.",
			Notes: []string{"DSX must be closed for this. If it is running, close it now (DSX tray icon > Exit): " +
				"EDSense adds it as soon as DSX is closed. Then start DSX again."},
			OK:  "Add",
			Key: key(control.AppDSX, l.shown, "add"),
		}
	}
	st.Files = slices.Clone(dsx.ProfileFiles)
	return st
}

// ds4Seen is what a look at DS4Windows' files found.
type ds4Seen struct {
	in      ds4w.Input
	plan    ds4w.Plan
	running bool   // DS4Windows runs
	backups bool   // the copies' folder is there
	shown   string // the data folder, as the player knows it
	port    int    // ds4windows_port in the settings
	at      time.Time

	appData, home string // %APPDATA% and %USERPROFILE%, for ShowDir
}

// Texts of DS4Windows' install steps, as the card and the question list
// them.
var stepTexts = map[string]string{
	ds4w.StepProfile:   "A profile called \"" + ds4w.ProfileName + "\" (" + ds4w.StepFile(ds4w.StepProfile) + ")",
	ds4w.StepRule:      "An Auto Profiles rule that loads it while Elite is in front (" + ds4w.StepFile(ds4w.StepRule) + ")",
	ds4w.StepListener:  "Settings > Game mod support (DSX) > \"Let game mods control triggers and lights\" on (" + ds4w.StepFile(ds4w.StepListener) + ")",
	ds4w.StepUDPServer: "Settings > UDP Server > Enable Server on, at 127.0.0.1, for EDSense's gyro aim (" + ds4w.StepFile(ds4w.StepUDPServer) + ")",
}

// ds4Profile is DS4Windows' profile card: the job j over the files l.
// done: a job that is done is shown, until DS4Windows has run since.
func ds4Profile(l ds4Seen, j ds4w.Job, done bool) control.ProfileState {
	st := emptyState(control.AppDS4Windows)
	if l.at.IsZero() && j.State == "" {
		st.State, st.Text = control.ProfileUnknown, "Checking DS4Windows' settings..."
		return st
	}
	p := l.plan
	st.Dir, st.Backups = l.shown, l.backups
	for _, s := range p.Steps {
		st.Steps = append(st.Steps, control.ProfileStep{ID: s.ID, State: s.State, Text: stepTexts[s.ID]})
	}
	switch p.State {
	case ds4w.PlanBlocked:
		st.State, st.Block = control.ProfileBlocked, p.Block
		switch p.Block {
		case ds4w.BlockOld:
			st.Text, st.Link = "This DS4Windows has no game mod support. EDSense needs DS4Windows 5.", "ds4windows_releases"
			if p.Version != "" {
				st.Text = fmt.Sprintf("DS4Windows %s has no game mod support. EDSense needs DS4Windows 5.", p.Version)
			}
		case ds4w.BlockLab:
			st.Text = "This is DS4Windows' portable lab, which EDSense does not set up."
		case ds4w.BlockTwo:
			st.Text = fmt.Sprintf("DS4Windows has settings in two folders, %s and %s, and asks at each start which one to use. "+
				"EDSense cannot tell which one it uses, so it leaves both alone. Keep only the one you use, then check again.",
				l.shown, l.showDir(l.in.Also))
		case ds4w.BlockUnreadable:
			st.Text = fmt.Sprintf("%s could not be read (%s), so EDSense leaves it alone. Set it up in DS4Windows instead.", p.File, p.Why)
		default:
			st.Text = "DS4Windows' settings were not found. Start DS4Windows once (it creates them), close it, then try again."
		}
	case ds4w.PlanMissing, ds4w.PlanPartial:
		st.State = map[string]string{ds4w.PlanMissing: control.ProfileMissing, ds4w.PlanPartial: control.ProfilePartial}[p.State]
		st.Text = "EDSense can add a DS4Windows profile for Elite: DualSense emulation, with the gyro and touchpad passed through, loaded whenever Elite is in front."
		switch listener, udp := settingsOnly(p.Todo(false)); {
		case listener && udp:
			st.Text = "Elite uses EDSense's profile \"" + ds4w.ProfileName + "\", but DS4Windows' game mod support and UDP server are off: " +
				"EDSense sends the triggers and lights through the first, and reads the gyro from the second. EDSense can turn them on."
		case listener:
			st.Text = "Elite uses EDSense's profile \"" + ds4w.ProfileName + "\", but DS4Windows' game mod support is off, " +
				"and EDSense sends the triggers and lights through it. EDSense can turn it on."
		case udp:
			// DS4Windows writes Enable Server to the file only when it exits
			st.Text = "Elite uses EDSense's profile \"" + ds4w.ProfileName + "\", but DS4Windows' UDP server is off in its settings file, " +
				"which DS4Windows writes when it exits. " +
				"EDSense's gyro aim reads the controller's motion from it, since DS4Windows' virtual DualSense drops every turn under 2 degrees per second. " +
				"EDSense can turn it on."
		}
	case ds4w.PlanOurs:
		st.State = control.ProfileOurs
		st.Text = "Elite uses EDSense's profile \"" + ds4w.ProfileName + "\". You can change it in DS4Windows: EDSense leaves it as it is."
	case ds4w.PlanOther:
		st.State = control.ProfileOther
		facts := p.Usual
		if p.Rule != nil && p.Rule.Profile != "" {
			st.Player, facts = p.Rule.Profile, p.Player
			st.Text = "Elite has its own Auto Profiles rule in DS4Windows (profile \"" + st.Player + "\"). EDSense leaves it alone."
		} else {
			st.Text = "Elite has its own Auto Profiles rule in DS4Windows, which keeps the controller's own profile. EDSense leaves it alone."
		}
		st.Items = factItems(facts)
	case ds4w.PlanUsualOK:
		st.State, st.Player = control.ProfileUsualOK, p.Usual.Name
		st.Text = "Your profile \"" + p.Usual.Name + "\" already works with EDSense."
	}
	if p.RuleLoses && p.Rule != nil {
		// DS4Windows picks EDSense's rule for the DualSense
		mine := "Your own Auto Profiles rule for Elite"
		if p.Rule.Profile != "" {
			st.Player = p.Rule.Profile
			mine += " (profile \"" + p.Rule.Profile + "\")"
		}
		st.Text += " " + mine + " is not used for the DualSense: DS4Windows picks EDSense's rule, made for a DualSense, before it. " +
			"To use yours, delete EDSense's rule in DS4Windows' Auto Profiles tab."
	}
	if p.State != ds4w.PlanBlocked {
		st.CanInstall, st.CanReset = p.CanInstall(), p.CanReset()
		switch {
		case st.CanInstall:
			st.Files, st.Install = p.Files(false), ds4Question(l)
		case st.CanReset:
			st.Files = p.Files(true)
		}
		if st.CanReset {
			st.Reset = &control.Confirm{
				Title: "Reset the DS4Windows profile?",
				Text: "Replace \"" + ds4w.ProfileName + "\" with EDSense's version? Changes you made to it in DS4Windows are lost. " +
					"A copy goes to " + DS4WindowsBackups + ".",
				Notes:  []string{"DS4Windows must be closed while EDSense writes it: its tray icon > Exit."},
				OK:     "Replace",
				Danger: true,
				Key:    key(control.AppDS4Windows, l.shown, "reset", p.Todo(true)...),
			}
		}
	}

	// a job that failed or stopped is shown while what it was to write is
	// still to do; the player may have set it up by hand since
	left := len(p.Todo(j.Reset)) > 0
	switch {
	case j.State == ds4w.JobWaiting:
		st.State, st.CanInstall, st.CanReset, st.CanCancel = control.ProfileWaiting, false, false, !j.Writing
		st.Text = "Waiting for DS4Windows to close. Use its tray icon > Exit: closing its window may only hide it."
		if j.Writing {
			st.State, st.Text = control.ProfileWriting, "Writing DS4Windows' settings now."
		}
		st.Install, st.Reset, st.Files = nil, nil, []string{}
		for _, id := range j.Steps {
			if f := ds4w.StepFile(id); !slices.Contains(st.Files, f) {
				st.Files = append(st.Files, f)
			}
		}
		st.Block, st.Link, st.Items = "", "", []control.Item{}
		st.Dir = l.showDir(j.Dir)
	case j.State == ds4w.JobDone && done:
		st.State, st.CanInstall, st.CanReset, st.CanCancel = control.ProfileDone, false, false, false
		st.Install, st.Reset, st.Files, st.Block, st.Link, st.Items = nil, nil, []string{}, "", "", []control.Item{}
		st.Text = doneText(j.Result)
	case j.State == ds4w.JobFailed && left:
		st.State, st.Text = control.ProfileFailed, "The DS4Windows settings could not be written: "+sentence(j.Err)
		if w := wroteBefore(j.Result); w != "" {
			st.Text += " " + w
		}
	case j.State == ds4w.JobInterrupted && left:
		st.State = control.ProfileInterrupted
		st.Text = "DS4Windows started while EDSense was writing, so only some of it is written. Exit DS4Windows and press Install again."
	}
	st.Files = orEmpty(st.Files)
	return st
}

// settingsOnly: the steps to do are only those of DS4Windows' settings,
// and which of them: game mod support's, the UDP server's. Both false
// when there are other steps, or none.
func settingsOnly(todo []string) (listener, udp bool) {
	for _, id := range todo {
		switch id {
		case ds4w.StepListener:
			listener = true
		case ds4w.StepUDPServer:
			udp = true
		default:
			return false, false
		}
	}
	return listener, udp
}

// settingsTurnedOn names the settings in wrote, and the verb that goes
// with them: game mod support, the UDP server or both.
func settingsTurnedOn(wrote []string) (names, verb string) {
	switch listener, udp := slices.Contains(wrote, ds4w.StepListener), slices.Contains(wrote, ds4w.StepUDPServer); {
	case listener && udp:
		return "game mod support and UDP server", "are"
	case udp:
		return "UDP server", "is"
	}
	return "game mod support", "is"
}

// wroteBefore names the files an install that failed wrote before it
// failed, which stay as written; "" when there are none.
func wroteBefore(r ds4w.Result) string {
	if len(r.Wrote) == 0 {
		return ""
	}
	var files []string
	for _, id := range r.Wrote {
		if f := ds4w.StepFile(id); !slices.Contains(files, f) {
			files = append(files, f)
		}
	}
	return "Written before that, and left as written: " + strings.Join(files, ", ") + "."
}

// doneText is the card's text once an install wrote r.
func doneText(r ds4w.Result) string {
	if slices.Contains(r.Wrote, ds4w.StepProfile) || slices.Contains(r.Wrote, ds4w.StepRule) {
		return "Written. Start DS4Windows again: Elite gets the \"" + ds4w.ProfileName + "\" profile whenever it is in front."
	}
	names, verb := settingsTurnedOn(r.Wrote)
	return "Written. Start DS4Windows again: its " + names + " " + verb + " on."
}

// doneMessage tells the player, wherever they are, that an install wrote
// r; "" when it wrote nothing.
func doneMessage(r ds4w.Result) string {
	switch {
	case slices.Contains(r.Wrote, ds4w.StepProfile) || slices.Contains(r.Wrote, ds4w.StepRule):
		return "The DS4Windows profile for Elite is written. Start DS4Windows again to use it."
	case len(r.Wrote) > 0:
		names, verb := settingsTurnedOn(r.Wrote)
		return "DS4Windows' " + names + " " + verb + " turned on. Start DS4Windows again to use it."
	}
	return ""
}

// ds4Question is the question before an install: what is written where,
// and what changes for the player.
func ds4Question(l ds4Seen) *control.Confirm {
	p := l.plan
	todo := p.Todo(false)
	what := todo
	if slices.Contains(todo, ds4w.StepUDPServer) && p.UDPMoves != "" {
		// the address the note names: one that moves later asks again
		what = append(slices.Clone(todo), "moves="+p.UDPMoves)
	}
	q := &control.Confirm{
		Title: "Install the DS4Windows profile for Elite?",
		Text:  "EDSense writes these in " + l.shown + ", and keeps copies of the files in " + DS4WindowsBackups + " next to EDSense:",
		Notes: []string{"DS4Windows must be closed while EDSense writes them: its tray icon > Exit."},
		OK:    "Install",
		Key:   key(control.AppDS4Windows, l.shown, "install", what...),
	}
	if listener, udp := settingsOnly(todo); listener || udp {
		q.Title, q.OK = "Turn on DS4Windows' game mod support?", "Turn on"
		switch {
		case listener && udp:
			q.Title = "Turn on DS4Windows' game mod support and UDP server?"
		case udp:
			q.Title = "Turn on DS4Windows' UDP server?"
		}
		q.Text = "EDSense changes this in " + l.shown + ", and keeps copies of the files in " + DS4WindowsBackups + " next to EDSense:"
		q.Notes[0] = "DS4Windows must be closed while EDSense writes it: its tray icon > Exit."
	}
	for _, id := range todo {
		q.Items = append(q.Items, stepTexts[id])
	}
	if slices.Contains(todo, ds4w.StepProfile) || slices.Contains(todo, ds4w.StepRule) {
		q.Notes = append(q.Notes, "While Elite is in front, your usual profile's button mapping, stick settings and lightbar are not used.")
		if p.Usual.Gyro == ds4w.GyroMouse {
			q.Notes = append(q.Notes, "EDSense's gyro aim replaces DS4Windows' gyro mouse in Elite.")
		}
		if p.Usual.Read && !ds4w.EmulatesDualSense(p.Usual.Output) {
			q.Notes = append(q.Notes, "Your usual profile emulates another controller, so DS4Windows plugs the virtual controller in again "+
				"each time Elite comes to the front or goes to the back.")
		}
	}
	if slices.Contains(todo, ds4w.StepUDPServer) && p.UDPMoves != "" {
		q.Notes = append(q.Notes, "DS4Windows' UDP server listens on "+p.UDPMoves+" now. EDSense sets it to 127.0.0.1, "+
			"so programs on other devices can no longer reach it.")
	}
	if slices.Contains(todo, ds4w.StepListener) && l.port != 0 {
		if _, port, err := net.SplitHostPort(p.Endpoint); err == nil && port != strconv.Itoa(l.port) {
			q.Notes = append(q.Notes, fmt.Sprintf("Game mod support keeps DS4Windows' own address, %s, while EDSense sends to port %d "+
				"(ds4windows_port in edsense.json). Set ds4windows_port to 0 so EDSense follows DS4Windows.", p.Endpoint, l.port))
		}
	}
	return q
}

// factItems are the checks of a profile EDSense found in DS4Windows'
// files: the card's view of the player's own profile.
func factItems(f ds4w.Facts) []control.Item {
	w := backend.DS4WindowsWords()
	switch {
	case f.Name == "":
		return []control.Item{item(IDProfile, control.ItemUnknown, "EDSense could not tell which profile the controller has")}
	case !f.Read:
		return []control.Item{item(IDProfile, control.ItemUnknown, fmt.Sprintf("The profile %q could not be read (%s)", f.Name, f.Problem))}
	}
	name := strconv.Quote(f.Name)
	profile := item(IDProfile, control.ItemOK, fmt.Sprintf("The profile %s emulates a DualSense", name))
	if !ds4w.EmulatesDualSense(f.Output) {
		profile.State = control.ItemBad
		profile.Text, profile.How = split(w.Warning(ds4w.WarnNotDualSense, fmt.Sprintf("%s (%s)", name, f.Output)))
	}
	const passthru = "For EDSense's gyro aim, set the profile's Gyro > Output Mode to Passthru."
	gyro := item(IDGyro, control.ItemUnknown, "EDSense cannot tell what the profile does with the gyro")
	switch f.Gyro {
	case ds4w.GyroFree:
		gyro = item(IDGyro, control.ItemOK, fmt.Sprintf("The profile %s leaves the gyro free, so EDSense aims", name))
	case ds4w.GyroMouse:
		gyro = item(IDGyro, control.ItemWarn, fmt.Sprintf("The profile %s uses the gyro as a mouse, so EDSense's gyro stays off", name))
	case ds4w.GyroOther:
		gyro = item(IDGyro, control.ItemWarn, fmt.Sprintf("The profile %s uses the gyro itself, so EDSense's gyro stays off", name))
	}
	if gyro.State != control.ItemOK {
		gyro.How = passthru
	}
	touch := item(IDTouchpad, control.ItemOK, "The touchpad is not a mouse")
	if (ds4w.Profile{Touchpad: f.Touchpad}).TouchpadMouse() {
		touch.State = control.ItemWarn
		touch.Text, touch.How = split(w.Warning(ds4w.WarnTouchpadMouse, name))
	}
	lab := item(IDTriggerLab, control.ItemOK, "Trigger Lab is off")
	if f.TriggerLab {
		lab.State = control.ItemWarn
		lab.Text, lab.How = split(w.Warning(ds4w.WarnTriggerLab, name))
	}
	return []control.Item{profile, gyro, touch, lab}
}

// showDir names a data folder for the window.
func (l ds4Seen) showDir(dir string) string {
	if dir == "" {
		return ""
	}
	return ds4w.ShowDir(dir, l.appData, l.home)
}

func emptyState(app string) control.ProfileState {
	return control.ProfileState{App: app, Steps: []control.ProfileStep{}, Files: []string{}, Items: []control.Item{}}
}

// sentence is msg as the end of a sentence.
func sentence(msg string) string {
	if msg = strings.TrimSpace(msg); msg == "" || strings.HasSuffix(msg, ".") {
		return msg
	}
	return msg + "."
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
