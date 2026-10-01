package install

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
)

// A player's folders, in this system's form, so ShowDir works on them.
var (
	testHome    = filepath.Join("C:", "Users", "p")
	testAppData = filepath.Join(testHome, "AppData", "Roaming")
	testDS4Dir  = filepath.Join(testAppData, "DS4Windows")
)

func plan(state string, profile, rule, listener string) ds4w.Plan {
	p := ds4w.Plan{Dir: testDS4Dir, State: state, Version: "5.0.12.0", Endpoint: "127.0.0.1:6969"}
	if state != ds4w.PlanBlocked {
		p.Steps = []ds4w.Step{{ID: ds4w.StepProfile, State: profile}, {ID: ds4w.StepRule, State: rule}, {ID: ds4w.StepListener, State: listener}}
	}
	return p
}

func seen(p ds4w.Plan) ds4Seen {
	return ds4Seen{in: ds4w.Input{Dir: p.Dir}, plan: p, shown: `%APPDATA%\DS4Windows`, at: time.Now(),
		appData: testAppData, home: testHome}
}

// plainText: the card's texts are ASCII with no control characters but
// the line ends of a question.
func plainText(t *testing.T, st control.ProfileState) {
	t.Helper()
	var texts []string
	texts = append(texts, st.Text)
	for _, s := range st.Steps {
		texts = append(texts, s.Text)
	}
	for _, it := range st.Items {
		texts = append(texts, it.Text, it.How)
	}
	for _, q := range []*control.Confirm{st.Install, st.Reset} {
		if q != nil {
			texts = append(texts, q.Title, q.Text, q.OK)
			texts = append(texts, q.Items...)
			texts = append(texts, q.Notes...)
		}
	}
	for _, s := range texts {
		for _, r := range s {
			if r > 0x7e || r < 0x20 && r != '\n' {
				t.Errorf("%q is not plain ASCII", s)
			}
		}
	}
	if st.Steps == nil || st.Files == nil || st.Items == nil {
		t.Errorf("nil lists: %+v", st)
	}
}

// TestDS4Profile: the card for each plan and job.
func TestDS4Profile(t *testing.T) {
	todo, done, skip := ds4w.StepTodo, ds4w.StepDone, ds4w.StepSkip
	blocked := func(block, file, why, version string) ds4w.Plan {
		p := plan(ds4w.PlanBlocked, "", "", "")
		p.Block, p.File, p.Why, p.Version = block, file, why, version
		return p
	}
	other := plan(ds4w.PlanOther, skip, skip, todo)
	other.Rule = &ds4w.Rule{Index: 1, Path: `C:\Games\EliteDangerous64.exe`, Profile: "My Elite"}
	other.Player = ds4w.Facts{Name: "My Elite", Read: true, Output: "ViiperX360", Gyro: ds4w.GyroMouse, Touchpad: "Mouse"}
	otherOwn := plan(ds4w.PlanOther, skip, skip, done)
	otherOwn.Rule = &ds4w.Rule{Index: 2, Title: "Elite"}
	otherOwn.Usual = ds4w.Facts{Name: "Default", Read: true, Output: "ViiperDualSense", Gyro: ds4w.GyroFree}
	usual := plan(ds4w.PlanUsualOK, skip, skip, todo)
	usual.Usual = ds4w.Facts{Name: "Mine", Read: true, Output: "ViiperDualSense", Gyro: ds4w.GyroFree}
	loses := plan(ds4w.PlanOurs, done, done, done)
	loses.Rule, loses.RuleLoses = &ds4w.Rule{Index: 1, Path: "EliteDangerous64.exe", Profile: "Mine"}, true
	all := []string{"profile", "rule", "listener"}
	ourFile := `Profiles\Elite Dangerous (EDSense).xml`
	oursText := `Elite uses EDSense's profile "Elite Dangerous (EDSense)". You can change it in DS4Windows: EDSense leaves it as it is.`

	cases := []struct {
		name                   string
		plan                   ds4w.Plan
		job                    ds4w.Job
		shown                  bool
		state, text            string
		install, reset, cancel bool
		player, block, link    string
		files                  []string
		items                  []string // the items' states
	}{
		{name: "no folder", plan: blocked(ds4w.BlockFolder, "Profiles.xml", "", ""), state: "blocked", block: "folder",
			text: "DS4Windows' settings were not found. Start DS4Windows once (it creates them), close it, then try again."},
		{name: "old", plan: blocked(ds4w.BlockOld, "", "", "3.3.3"), state: "blocked", block: "old", link: "ds4windows_releases",
			text: "DS4Windows 3.3.3 has no game mod support. EDSense needs DS4Windows 5."},
		{name: "old, no version", plan: blocked(ds4w.BlockOld, "", "", ""), state: "blocked", block: "old", link: "ds4windows_releases",
			text: "This DS4Windows has no game mod support. EDSense needs DS4Windows 5."},
		{name: "lab", plan: blocked(ds4w.BlockLab, "", "", ""), state: "blocked", block: "lab",
			text: "This is DS4Windows' portable lab, which EDSense does not set up."},
		{name: "utf-16", plan: blocked(ds4w.BlockUnreadable, "Profiles.xml", "it is saved as UTF-16", ""), state: "blocked", block: "unreadable",
			text: "Profiles.xml could not be read (it is saved as UTF-16), so EDSense leaves it alone. Set it up in DS4Windows instead."},
		{name: "missing", plan: plan(ds4w.PlanMissing, todo, todo, todo), state: "missing", install: true,
			text:  "EDSense can add a DS4Windows profile for Elite: DualSense emulation, with the gyro and touchpad passed through, loaded whenever Elite is in front.",
			files: []string{`Profiles\Elite Dangerous (EDSense).xml`, "Auto Profiles.xml", "Profiles.xml"}},
		{name: "partial", plan: plan(ds4w.PlanPartial, todo, done, done), state: "partial", install: true,
			text:  "EDSense can add a DS4Windows profile for Elite: DualSense emulation, with the gyro and touchpad passed through, loaded whenever Elite is in front.",
			files: []string{ourFile}},
		{name: "partial, game mods only", plan: plan(ds4w.PlanPartial, done, done, todo), state: "partial", install: true, reset: true,
			text: `Elite uses EDSense's profile "Elite Dangerous (EDSense)", but DS4Windows' game mod support is off, ` +
				"and EDSense sends the triggers and lights through it. EDSense can turn it on.",
			files: []string{"Profiles.xml"}},
		{name: "ours, the player's rule loses", plan: loses, state: "ours", reset: true, player: "Mine", files: []string{ourFile},
			text: oursText + ` Your own Auto Profiles rule for Elite (profile "Mine") is not used for the DualSense: ` +
				"DS4Windows picks EDSense's rule, made for a DualSense, before it. To use yours, delete EDSense's rule in DS4Windows' Auto Profiles tab."},
		{name: "ours", plan: plan(ds4w.PlanOurs, done, done, done), state: "ours", reset: true,
			text:  `Elite uses EDSense's profile "Elite Dangerous (EDSense)". You can change it in DS4Windows: EDSense leaves it as it is.`,
			files: []string{`Profiles\Elite Dangerous (EDSense).xml`}},
		{name: "other", plan: other, state: "other", install: true, player: "My Elite",
			text:  `Elite has its own Auto Profiles rule in DS4Windows (profile "My Elite"). EDSense leaves it alone.`,
			files: []string{"Profiles.xml"}, items: []string{"bad", "warn", "warn", "ok"}},
		{name: "other, the controller's own profile", plan: otherOwn, state: "other",
			text:  "Elite has its own Auto Profiles rule in DS4Windows, which keeps the controller's own profile. EDSense leaves it alone.",
			items: []string{"ok", "ok", "ok", "ok"}},
		{name: "usual ok", plan: usual, state: "usual_ok", install: true, player: "Mine",
			text: `Your profile "Mine" already works with EDSense.`, files: []string{"Profiles.xml"}},
		{name: "waiting", plan: plan(ds4w.PlanMissing, todo, todo, todo), job: ds4w.Job{State: ds4w.JobWaiting, Dir: testDS4Dir, Steps: all},
			state: "waiting", cancel: true, files: []string{`Profiles\Elite Dangerous (EDSense).xml`, "Auto Profiles.xml", "Profiles.xml"},
			text: "Waiting for DS4Windows to close. Use its tray icon > Exit: closing its window may only hide it."},
		{name: "waiting for a reset", plan: plan(ds4w.PlanOurs, done, done, done), job: ds4w.Job{State: ds4w.JobWaiting, Reset: true, Dir: testDS4Dir, Steps: all[:1]},
			state: "waiting", cancel: true, files: []string{`Profiles\Elite Dangerous (EDSense).xml`},
			text: "Waiting for DS4Windows to close. Use its tray icon > Exit: closing its window may only hide it."},
		// the files a waiting job writes are the ones asked for
		{name: "waiting, the plan grew", plan: plan(ds4w.PlanPartial, done, todo, todo),
			job:   ds4w.Job{State: ds4w.JobWaiting, Dir: testDS4Dir, Steps: all[2:]},
			state: "waiting", cancel: true, files: []string{"Profiles.xml"},
			text: "Waiting for DS4Windows to close. Use its tray icon > Exit: closing its window may only hide it."},
		{name: "writing", plan: plan(ds4w.PlanMissing, todo, todo, todo),
			job:   ds4w.Job{State: ds4w.JobWaiting, Dir: testDS4Dir, Steps: all, Writing: true},
			state: "writing", files: []string{ourFile, "Auto Profiles.xml", "Profiles.xml"}, text: "Writing DS4Windows' settings now."},
		{name: "done", plan: plan(ds4w.PlanOurs, done, done, done), shown: true,
			job:   ds4w.Job{State: ds4w.JobDone, Result: ds4w.Result{Wrote: []string{"profile", "rule", "listener"}}},
			state: "done", text: `Written. Start DS4Windows again: Elite gets the "Elite Dangerous (EDSense)" profile whenever it is in front.`},
		{name: "done, game mods only", plan: plan(ds4w.PlanOther, skip, skip, done), shown: true,
			job:   ds4w.Job{State: ds4w.JobDone, Result: ds4w.Result{Wrote: []string{"listener"}}},
			state: "done", text: "Written. Start DS4Windows again: its game mod support is on."},
		{name: "done, seen through", plan: plan(ds4w.PlanOurs, done, done, done), reset: true,
			job:   ds4w.Job{State: ds4w.JobDone, Result: ds4w.Result{Wrote: []string{"profile"}}},
			state: "ours", files: []string{`Profiles\Elite Dangerous (EDSense).xml`},
			text: `Elite uses EDSense's profile "Elite Dangerous (EDSense)". You can change it in DS4Windows: EDSense leaves it as it is.`},
		{name: "failed", plan: plan(ds4w.PlanMissing, todo, todo, todo), install: true,
			job:   ds4w.Job{State: ds4w.JobFailed, Err: "Auto Profiles.xml was not written right (x), so it was put back from its copy", Result: ds4w.Result{Restored: true}},
			state: "failed", files: []string{ourFile, "Auto Profiles.xml", "Profiles.xml"},
			text: "The DS4Windows settings could not be written: Auto Profiles.xml was not written right (x), so it was put back from its copy."},
		{name: "failed after a write", plan: plan(ds4w.PlanPartial, done, done, todo), install: true, reset: true,
			job: ds4w.Job{State: ds4w.JobFailed, Err: "Profiles.xml could not be written (x), so it was left as it was",
				Result: ds4w.Result{Wrote: all[:2]}},
			state: "failed", files: []string{"Profiles.xml"},
			text: "The DS4Windows settings could not be written: Profiles.xml could not be written (x), so it was left as it was. " +
				`Written before that, and left as written: Profiles\Elite Dangerous (EDSense).xml, Auto Profiles.xml.`},
		// set up by hand since: the job's failure is over
		{name: "failed, then set up by hand", plan: otherOwn, state: "other", job: ds4w.Job{State: ds4w.JobFailed, Err: "x"},
			text:  "Elite has its own Auto Profiles rule in DS4Windows, which keeps the controller's own profile. EDSense leaves it alone.",
			items: []string{"ok", "ok", "ok", "ok"}},
		{name: "failed, then all there", plan: plan(ds4w.PlanOurs, done, done, done), reset: true, files: []string{ourFile},
			job: ds4w.Job{State: ds4w.JobFailed, Err: "x"}, state: "ours", text: oursText},
		{name: "a reset failed", plan: plan(ds4w.PlanOurs, done, done, done), reset: true, files: []string{ourFile},
			job:   ds4w.Job{State: ds4w.JobFailed, Reset: true, Err: "x"},
			state: "failed", text: "The DS4Windows settings could not be written: x."},
		{name: "interrupted, then finished by hand", plan: plan(ds4w.PlanOurs, done, done, done), reset: true, files: []string{ourFile},
			job: ds4w.Job{State: ds4w.JobInterrupted, Err: "x"}, state: "ours", text: oursText},
		{name: "interrupted", plan: plan(ds4w.PlanPartial, done, todo, todo), install: true, reset: false,
			job:   ds4w.Job{State: ds4w.JobInterrupted, Err: "x"},
			state: "interrupted", files: []string{"Auto Profiles.xml", "Profiles.xml"},
			text: "DS4Windows started while EDSense was writing, so only some of it is written. Exit DS4Windows and press Install again."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := ds4Profile(seen(c.plan), c.job, c.shown)
			if st.App != "ds4windows" || st.State != c.state || st.Text != c.text || st.Block != c.block || st.Link != c.link || st.Player != c.player {
				t.Errorf("card %q %q %q %q %q:\n%s", st.State, st.Block, st.Link, st.Player, st.Text, st.Text)
			}
			if st.CanInstall != c.install || st.CanReset != c.reset || st.CanCancel != c.cancel {
				t.Errorf("install %v reset %v cancel %v", st.CanInstall, st.CanReset, st.CanCancel)
			}
			if (st.Install != nil) != c.install || (st.Reset != nil) != c.reset {
				t.Errorf("questions: install %v, reset %v", st.Install != nil, st.Reset != nil)
			}
			if !slices.Equal(st.Files, orEmpty(c.files)) {
				t.Errorf("files %q", st.Files)
			}
			var items []string
			for _, it := range st.Items {
				items = append(items, it.State)
			}
			if !slices.Equal(items, c.items) {
				t.Errorf("items %q", items)
			}
			if c.plan.State != ds4w.PlanBlocked && len(st.Steps) != 3 || st.Dir != `%APPDATA%\DS4Windows` {
				t.Errorf("steps %v, dir %q", st.Steps, st.Dir)
			}
			plainText(t, st)
		})
	}
	if st := ds4Profile(ds4Seen{}, ds4w.Job{}, false); st.State != "unknown" || st.Text != "Checking DS4Windows' settings..." {
		t.Errorf("never looked at: %+v", st)
	}

	// settings in two folders: both are named, nothing is offered
	two := plan(ds4w.PlanBlocked, "", "", "")
	two.Block, two.Dir = ds4w.BlockTwo, `D:\Tools\DS4Windows`
	l := seen(two)
	l.shown, l.in.Also = two.Dir, testDS4Dir
	st := ds4Profile(l, ds4w.Job{}, false)
	if st.State != "blocked" || st.Block != "two" || st.CanInstall || st.CanReset || st.Text != `DS4Windows has settings in two folders, `+
		`D:\Tools\DS4Windows and %APPDATA%\DS4Windows, and asks at each start which one to use. EDSense cannot tell which one it uses, `+
		"so it leaves both alone. Keep only the one you use, then check again." {
		t.Errorf("two folders: %+v", st)
	}
}

// TestDS4Question: the install question names the folder and what is
// written, and says what changes for the player (H2) and a port that
// stays apart (L17).
func TestDS4Question(t *testing.T) {
	mapping := "While Elite is in front, your usual profile's button mapping, stick settings and lightbar are not used."
	gyro := "EDSense's gyro aim replaces DS4Windows' gyro mouse in Elite."
	replug := "Your usual profile emulates another controller"
	closed := "DS4Windows must be closed while EDSense writes them: its tray icon > Exit."

	p := plan(ds4w.PlanMissing, ds4w.StepTodo, ds4w.StepTodo, ds4w.StepTodo)
	p.Usual = ds4w.Facts{Name: "Mine", Read: true, Output: "ViiperX360", Gyro: ds4w.GyroMouse}
	l := seen(p)
	q := ds4Profile(l, ds4w.Job{}, false).Install
	if q.Title != "Install the DS4Windows profile for Elite?" || q.OK != "Install" || q.Danger ||
		q.Text != `EDSense writes these in %APPDATA%\DS4Windows, and keeps copies of the files in ds4windows_backups next to EDSense:` {
		t.Errorf("question %+v", q)
	}
	if !slices.Equal(q.Items, []string{
		`A profile called "Elite Dangerous (EDSense)" (Profiles\Elite Dangerous (EDSense).xml)`,
		"An Auto Profiles rule that loads it while Elite is in front (Auto Profiles.xml)",
		`Settings > Game mod support (DSX) > "Let game mods control triggers and lights" on (Profiles.xml)`,
	}) {
		t.Errorf("items %q", q.Items)
	}
	if len(q.Notes) != 4 || q.Notes[0] != closed || q.Notes[1] != mapping || q.Notes[2] != gyro || !strings.HasPrefix(q.Notes[3], replug) {
		t.Errorf("notes %q", q.Notes)
	}
	// the key names the folder and the steps: another one of either asks another question
	if q.Key != `ds4windows|%APPDATA%\DS4Windows|install|profile|rule|listener` {
		t.Errorf("key %q", q.Key)
	}
	l2 := l
	l2.shown = `D:\DS4Windows`
	if k := ds4Profile(l2, ds4w.Job{}, false).Install.Key; k == q.Key {
		t.Errorf("the same key in another folder: %q", k)
	}

	// a usual profile that emulates a DualSense with its gyro free
	p.Usual = ds4w.Facts{Name: "Mine", Read: true, Output: "ViiperDualSense", Gyro: ds4w.GyroFree}
	if q := ds4Profile(seen(p), ds4w.Job{}, false).Install; !slices.Equal(q.Notes, []string{closed, mapping}) {
		t.Errorf("notes %q", q.Notes)
	}

	// game mod support only, with ds4windows_port set apart
	p = plan(ds4w.PlanOther, ds4w.StepSkip, ds4w.StepSkip, ds4w.StepTodo)
	p.Rule, p.Endpoint = &ds4w.Rule{Profile: "My Elite"}, "127.0.0.1:6969"
	l = seen(p)
	l.port = 7000
	q = ds4Profile(l, ds4w.Job{}, false).Install
	if q.Title != "Turn on DS4Windows' game mod support?" || q.OK != "Turn on" ||
		q.Text != `EDSense changes this in %APPDATA%\DS4Windows, and keeps copies of the files in ds4windows_backups next to EDSense:` {
		t.Errorf("listener only: %+v", q)
	}
	if len(q.Items) != 1 || len(q.Notes) != 2 || q.Notes[0] != "DS4Windows must be closed while EDSense writes it: its tray icon > Exit." ||
		q.Notes[1] != "Game mod support keeps DS4Windows' own address, 127.0.0.1:6969, while EDSense sends to port 7000 "+
			"(ds4windows_port in edsense.json). Set ds4windows_port to 0 so EDSense follows DS4Windows." {
		t.Errorf("listener only: %+v", q)
	}
	l.port = 6969
	if q := ds4Profile(l, ds4w.Job{}, false).Install; len(q.Notes) != 1 || q.Key != `ds4windows|%APPDATA%\DS4Windows|install|listener` {
		t.Errorf("the same port: %q, key %q", q.Notes, q.Key)
	}

	r := ds4Profile(seen(plan(ds4w.PlanOurs, ds4w.StepDone, ds4w.StepDone, ds4w.StepDone)), ds4w.Job{}, false).Reset
	if r == nil || !r.Danger || r.OK != "Replace" || r.Text != `Replace "Elite Dangerous (EDSense)" with EDSense's version? `+
		"Changes you made to it in DS4Windows are lost. A copy goes to ds4windows_backups." {
		t.Errorf("reset %+v", r)
	}
	if r.Key != `ds4windows|%APPDATA%\DS4Windows|reset|profile` {
		t.Errorf("reset key %q", r.Key)
	}
}

// TestDSXProfile: the card for each state of DSX's installer.
func TestDSXProfile(t *testing.T) {
	folder := `D:\SteamLibrary\steamapps\common\DSX\3.1`
	l := func(has bool, forElite string) dsxSeen {
		return dsxSeen{look: dsx.Look{Folder: folder, Has: has, ForElite: forElite}, shown: folder, at: time.Now()}
	}
	auto := func(s dsxSeen) dsxSeen { s.autoAdd = true; return s }
	cases := []struct {
		name                   string
		seen                   dsxSeen
		job                    dsx.Job
		state, text, player    string
		install, reset, cancel bool
	}{
		{"no folder", dsxSeen{at: time.Now()}, dsx.Job{}, "no_folder", "DSX's folder was not found, so the profile could not be written.", "", false, false, false},
		{"missing", l(false, ""), dsx.Job{}, "missing",
			"EDSense comes with a DSX profile for Elite Dangerous (gyro aim, touchpad and trigger setup). Install... adds it.", "", true, false, false},
		{"missing, the first add to come", auto(l(false, "")), dsx.Job{}, "missing",
			"EDSense comes with a DSX profile for Elite Dangerous (gyro aim, touchpad and trigger setup). It is added when DSX is closed.", "", true, false, false},
		{"present", l(true, "Elite Dangerous"), dsx.Job{}, "present", `DSX has an "Elite Dangerous" profile, set for Elite.`, "", false, true, false},
		{"not for elite", l(true, "Mine"), dsx.Job{}, "not_for_elite", `DSX has an "Elite Dangerous" profile, but Elite gets the profile "Mine".`, "Mine", false, true, false},
		{"no game profile", l(true, ""), dsx.Job{}, "not_for_elite", `DSX has an "Elite Dangerous" profile, but DSX does not pick it when Elite starts.`, "", false, true, false},
		{"waiting", l(true, "Elite Dangerous"), dsx.Job{State: dsx.JobWaiting, Reset: true}, "waiting", "Waiting for DSX to close (DSX tray icon > Exit).", "", false, false, true},
		{"done", l(true, "Elite Dangerous"), dsx.Job{State: dsx.JobDone}, "done", `The "Elite Dangerous" controller profile is now in DSX. Start DSX again to use it.`, "", false, false, false},
		{"failed", l(false, ""), dsx.Job{State: dsx.JobFailed, Err: "open Elite Dangerous.dsx: access is denied"}, "failed",
			"The DSX profile could not be written: open Elite Dangerous.dsx: access is denied.", "", true, false, false},
		{"failed, then added by hand", l(true, "Elite Dangerous"), dsx.Job{State: dsx.JobFailed, Err: "x"}, "present",
			`DSX has an "Elite Dangerous" profile, set for Elite.`, "", false, true, false},
		{"writing", l(false, ""), dsx.Job{State: dsx.JobWaiting, Writing: true}, "writing", "Writing the DSX profile now.", "", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := dsxProfile(c.seen, c.job)
			if st.App != "dsx" || st.State != c.state || st.Text != c.text || st.Player != c.player {
				t.Errorf("card %q %q: %s", st.State, st.Player, st.Text)
			}
			if st.CanInstall != c.install || st.CanReset != c.reset || st.CanCancel != c.cancel ||
				(st.Install != nil) != c.install || (st.Reset != nil) != c.reset {
				t.Errorf("install %v reset %v cancel %v", st.CanInstall, st.CanReset, st.CanCancel)
			}
			plainText(t, st)
		})
	}
	if st := dsxProfile(dsxSeen{}, dsx.Job{}); st.State != "unknown" {
		t.Errorf("never looked at: %+v", st)
	}
	if q := dsxProfile(l(true, "Elite Dangerous"), dsx.Job{}).Reset; q.Title != "Reset the DSX profile?" || !q.Danger || q.OK != "Reset" ||
		len(q.Notes) != 0 || q.Key != "dsx|"+folder+"|reset|Elite Dangerous" {
		t.Errorf("reset question %+v", q)
	}
	// a reset makes EDSense's profile Elite's game profile: the question says so
	if q := dsxProfile(l(true, "Mine"), dsx.Job{}).Reset; len(q.Notes) != 1 ||
		q.Notes[0] != `Elite's game profile in DSX changes from "Mine" to "Elite Dangerous" too. A copy of DSX's game profiles goes to dsx_profile_backups.` {
		t.Errorf("reset over the player's game profile: %q", q.Notes)
	}
	if q := dsxProfile(l(true, ""), dsx.Job{}).Reset; len(q.Notes) != 1 || !strings.Contains(q.Notes[0], "picks no profile for Elite now") {
		t.Errorf("reset with no game profile: %q", q.Notes)
	}
	if k1, k2 := dsxProfile(l(true, "Mine"), dsx.Job{}).Reset.Key, dsxProfile(l(true, "Other"), dsx.Job{}).Reset.Key; k1 == k2 {
		t.Errorf("one key for two questions: %q", k1)
	}
	if q := dsxProfile(l(false, ""), dsx.Job{}).Install; q.Key != "dsx|"+folder+"|add" {
		t.Errorf("install key %q", q.Key)
	}
}

// TestProfileStateJSON: the card's shape for the page.
func TestProfileStateJSON(t *testing.T) {
	b, err := json.Marshal(dsxProfile(dsxSeen{at: time.Now()}, dsx.Job{}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"app":"dsx","rev":0,"state":"no_folder","text":"DSX's folder was not found, so the profile could not be written.",` +
		`"steps":[],"files":[],"items":[],"backups":false,"can_install":false,"can_reset":false,"can_cancel":false}`
	if string(b) != want {
		t.Errorf("%s\nwant %s", b, want)
	}
}
