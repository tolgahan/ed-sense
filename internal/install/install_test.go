package install

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// states is each item's state, by id.
func states(items []control.Item) map[string]string {
	m := map[string]string{}
	for _, it := range items {
		m[it.ID] = it.State
	}
	return m
}

func byID(items []control.Item, id string) control.Item {
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	return control.Item{}
}

// in is EDSense on app, which runs, answers and lists one controller, with
// gyro aim on.
func in(app string) Inputs {
	seen := control.Seen{Running: true, Addr: "127.0.0.1:6969", Answers: app}
	i := Inputs{App: app,
		Status: control.Status{Full: true, Kind: app, Online: true, Controllers: 1,
			Gyro: control.GyroStatus{Aim: true, By: "edsense"}},
		Pads: Pads{Virtual: true}}
	if app == control.AppDSX {
		i.Seen.DSX = seen
		i.Profile = &control.ProfileState{App: app, State: control.ProfilePresent, CanReset: true}
	} else {
		seen.Version = "5.0.12.0"
		i.Seen.DS4Windows = seen
		i.Report = &ds4w.Report{Profile: "Elite", Output: "ViiperDualSense", Gyro: ds4w.GyroFree, Touchpad: "Passthru"}
	}
	return i
}

var (
	dsxIDs  = []string{"app", "listener", "controller", "virtual_pad", "dsx_profile"}
	ds4wIDs = []string{"app", "listener", "controller", "virtual_pad", "profile", "gyro", "touchpad", "trigger_lab", "hidhide"}
)

// TestChecklist: each item's state for what was gathered. Every list has
// the same items in the same order, with texts and nothing that is not
// ASCII.
func TestChecklist(t *testing.T) {
	cases := []struct {
		name string
		app  string
		edit func(i *Inputs)
		want map[string]string // the items that are not ok
	}{
		{"dsx ok", control.AppDSX, nil, nil},
		{"dsx not running", control.AppDSX, func(i *Inputs) {
			i.Seen.DSX = control.Seen{Addr: "127.0.0.1:6969"}
			i.Status.Online, i.Pads.Virtual = false, false
		}, map[string]string{"app": "bad", "listener": "wait", "controller": "wait", "virtual_pad": "wait"}},
		{"dsx no incoming udp", control.AppDSX, func(i *Inputs) { i.Seen.DSX.Answers = ""; i.Status.Online = false },
			map[string]string{"listener": "bad", "controller": "wait"}},
		{"dsx port taken", control.AppDSX, func(i *Inputs) { i.Seen.DSX.Answers = control.AppDS4Windows },
			map[string]string{"listener": "bad"}},
		{"dsx no controller", control.AppDSX, func(i *Inputs) { i.Status.Controllers = 0 }, map[string]string{"controller": "bad"}},
		{"dsx no emulation", control.AppDSX, func(i *Inputs) { i.Pads.Virtual = false }, map[string]string{"virtual_pad": "bad"}},
		{"dsx status not full", control.AppDSX, func(i *Inputs) { i.Status.Full = false }, map[string]string{"controller": "unknown"}},
		{"dsx while on ds4windows", control.AppDSX, func(i *Inputs) { i.Status.Kind = control.AppDS4Windows },
			map[string]string{"controller": "later"}},
		{"dsx profile not looked at", control.AppDSX, func(i *Inputs) { i.Profile = nil }, map[string]string{"dsx_profile": "unknown"}},
		{"dsx profile missing", control.AppDSX, func(i *Inputs) { i.Profile.State = control.ProfileMissing },
			map[string]string{"dsx_profile": "warn"}},
		{"dsx profile not for elite", control.AppDSX, func(i *Inputs) { i.Profile.State, i.Profile.Player = control.ProfileNotForElite, "Mine" },
			nil}, // the player's pick for Elite
		{"dsx profile for no game", control.AppDSX, func(i *Inputs) { i.Profile.State, i.Profile.Player = control.ProfileNotForElite, "" },
			map[string]string{"dsx_profile": "warn"}},
		{"dsx profile writing", control.AppDSX, func(i *Inputs) { i.Profile.State = control.ProfileWriting },
			map[string]string{"dsx_profile": "wait"}},
		{"dsx profile waiting", control.AppDSX, func(i *Inputs) { i.Profile.State = control.ProfileWaiting },
			map[string]string{"dsx_profile": "wait"}},
		{"dsx profile written", control.AppDSX, func(i *Inputs) { i.Profile.State = control.ProfileDone },
			map[string]string{"dsx_profile": "wait"}},
		{"dsx profile failed", control.AppDSX, func(i *Inputs) {
			i.Profile.State, i.Profile.Text = control.ProfileFailed, "The DSX profile could not be written: access is denied."
		}, map[string]string{"dsx_profile": "bad"}},
		{"dsx folder not found", control.AppDSX, func(i *Inputs) { i.Profile.State = control.ProfileNoFolder },
			map[string]string{"dsx_profile": "unknown"}},

		{"ds4windows ok", control.AppDS4Windows, nil, nil},
		{"ds4windows physical pad hidden, gyro aim off", control.AppDS4Windows, func(i *Inputs) {
			i.Status.Gyro.Aim = false
			i.Report.Gyro = ds4w.GyroMouse
		}, nil},
		{"ds4windows not running", control.AppDS4Windows, func(i *Inputs) {
			i.Seen.DS4Windows = control.Seen{Addr: "127.0.0.1:6969"}
			i.Status.Online, i.Pads.Virtual = false, false
		}, map[string]string{"app": "bad", "listener": "wait", "controller": "wait", "virtual_pad": "wait", "hidhide": "unknown"}},
		{"ds4windows 4", control.AppDS4Windows, func(i *Inputs) { i.Seen.DS4Windows.Version = "4.9.1.0" }, map[string]string{"app": "bad"}},
		{"ds4windows old by its files", control.AppDS4Windows, func(i *Inputs) {
			i.Seen.DS4Windows.Version = ""
			i.Report.Warnings = []ds4w.Warning{ds4w.WarnOldVersion}
		}, map[string]string{"app": "bad"}},
		// updated since the session started: the version seen now wins
		{"ds4windows updated to 5", control.AppDS4Windows, func(i *Inputs) {
			i.Report.Warnings = []ds4w.Warning{ds4w.WarnOldVersion}
		}, nil},
		{"ds4windows game mods off", control.AppDS4Windows, func(i *Inputs) { i.Seen.DS4Windows.Answers = ""; i.Status.Online = false },
			map[string]string{"listener": "bad", "controller": "wait", "hidhide": "unknown"}},
		{"dsx on ds4windows' port", control.AppDS4Windows, func(i *Inputs) { i.Seen.DS4Windows.Answers = control.AppDSX },
			map[string]string{"listener": "bad"}},
		{"ds4windows emulates an xbox pad", control.AppDS4Windows, func(i *Inputs) { i.Report.Output = "ViiperX360"; i.Pads.Virtual = false },
			map[string]string{"virtual_pad": "bad", "profile": "bad"}},
		{"ds4windows gyro mouse", control.AppDS4Windows, func(i *Inputs) { i.Report.Gyro = ds4w.GyroMouse }, map[string]string{"gyro": "warn"}},
		{"ds4windows gyro on a stick", control.AppDS4Windows, func(i *Inputs) { i.Report.Gyro = ds4w.GyroOther }, map[string]string{"gyro": "warn"}},
		{"ds4windows gyro unknown", control.AppDS4Windows, func(i *Inputs) { i.Report.Gyro = ds4w.GyroUnknown }, map[string]string{"gyro": "unknown"}},
		{"ds4windows gyro by the profile", control.AppDS4Windows, func(i *Inputs) {
			i.Status.Gyro.By = "dsx"
			i.Report.Gyro = ds4w.GyroMouse
		}, nil},
		{"ds4windows touchpad and trigger lab", control.AppDS4Windows, func(i *Inputs) {
			i.Report.Warnings = []ds4w.Warning{ds4w.WarnTriggerLab, ds4w.WarnTouchpadMouse}
		}, map[string]string{"touchpad": "warn", "trigger_lab": "warn"}},
		{"ds4windows real pad visible", control.AppDS4Windows, func(i *Inputs) { i.Pads.Physical = true }, map[string]string{"hidhide": "bad"}},
		{"ds4windows no controller", control.AppDS4Windows, func(i *Inputs) { i.Status.Controllers = 0 },
			map[string]string{"controller": "bad", "hidhide": "unknown"}},
		{"ds4windows before the first check", control.AppDS4Windows, func(i *Inputs) { i.Report = nil },
			map[string]string{"profile": "wait", "gyro": "wait", "touchpad": "wait", "trigger_lab": "wait"}},
		{"ds4windows profile not known", control.AppDS4Windows, func(i *Inputs) { i.Report = &ds4w.Report{Source: "no answer"} },
			map[string]string{"profile": "unknown", "gyro": "unknown", "touchpad": "unknown", "trigger_lab": "unknown"}},
		{"ds4windows profile unreadable", control.AppDS4Windows, func(i *Inputs) { i.Report = &ds4w.Report{Profile: "Gone", Problem: "no file"} },
			map[string]string{"profile": "unknown", "gyro": "unknown", "touchpad": "unknown", "trigger_lab": "unknown"}},
		{"ds4windows while on dsx", control.AppDS4Windows, func(i *Inputs) {
			i.Status.Kind = control.AppDSX
			i.Report = nil
		}, map[string]string{"controller": "later", "profile": "later", "gyro": "later", "touchpad": "later", "trigger_lab": "later", "hidhide": "later"}},
		{"ds4windows while on dsx, real pad visible", control.AppDS4Windows, func(i *Inputs) {
			i.Status.Kind = control.AppDSX
			i.Status.Gyro.Aim = false
			i.Pads.Physical = true
		}, map[string]string{"controller": "later", "profile": "later", "gyro": "later", "touchpad": "later", "trigger_lab": "later", "hidhide": "later"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			i := in(c.app)
			if c.edit != nil {
				c.edit(&i)
			}
			items := Checklist(i)
			var ids []string
			for _, it := range items {
				ids = append(ids, it.ID)
				if it.Text == "" || strings.HasSuffix(it.Text, ".") {
					t.Errorf("%s: text %q", it.ID, it.Text)
				}
				for _, s := range []string{it.Text, it.How} {
					for _, r := range s {
						if r > 0x7e || r < 0x20 {
							t.Errorf("%s: %q is not plain ASCII", it.ID, s)
						}
					}
				}
				if it.State != control.ItemOK && it.State != control.ItemLater && it.State != control.ItemWait &&
					it.State != control.ItemUnknown && it.How == "" && it.ID != IDGyro {
					t.Errorf("%s is %s with nothing to do about it: %q", it.ID, it.State, it.Text)
				}
				if it.Link != "" {
					if _, ok := control.Address(it.Link); !ok {
						t.Errorf("%s links %q", it.ID, it.Link)
					}
				}
			}
			want := dsxIDs
			if c.app == control.AppDS4Windows {
				want = ds4wIDs
			}
			if !slices.Equal(ids, want) {
				t.Fatalf("items %q", ids)
			}
			got := states(items)
			for id, st := range got {
				w := c.want[id]
				if w == "" {
					w = control.ItemOK
				}
				if st != w {
					t.Errorf("%s: %s (%q), want %s", id, st, byID(items, id).Text, w)
				}
			}
		})
	}
}

// TestChecklistTexts: what the player reads in a few cases.
func TestChecklistTexts(t *testing.T) {
	i := in(control.AppDS4Windows)
	items := Checklist(i)
	for id, want := range map[string]string{
		"app":         "DS4Windows 5.0.12.0 runs",
		"listener":    "Game mod support answers on 127.0.0.1:6969",
		"controller":  "DS4Windows lists the controller",
		"profile":     `The profile "Elite" emulates a DualSense`,
		"gyro":        `The profile "Elite" leaves the gyro free, so EDSense aims`,
		"hidhide":     "Games see only the virtual DualSense",
		"trigger_lab": "Trigger Lab is off",
	} {
		if got := byID(items, id).Text; got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}

	i.Seen.DS4Windows.Answers = ""
	i.Report.Output = "ViiperX360"
	i.Report.Warnings = []ds4w.Warning{ds4w.WarnTriggerLab}
	i.Pads.Physical = true
	i.Status.Controllers = 2
	items = Checklist(i)
	for id, want := range map[string][3]string{
		"listener": {"Game mod support does not answer on 127.0.0.1:6969",
			`In DS4Windows, tick Settings > Game mod support (DSX) > "Let game mods control triggers and lights".`, "ds4windows_doc"},
		"controller": {"DS4Windows lists 2 controllers", "", ""},
		"profile": {`The DS4Windows profile "Elite" (ViiperX360) does not emulate a DualSense, so EDSense cannot read the controller or play its haptics`,
			"In DS4Windows, edit the profile: Advanced > Emulated Controller > DualSense.", ""},
		"trigger_lab": {`The DS4Windows profile "Elite" has Trigger Lab on, which wins over EDSense's triggers on that side`,
			"Turn Trigger Lab off in the profile to feel EDSense's triggers.", ""},
		"hidhide": {"Games can see your real DualSense next to DS4Windows' virtual one, so Elite may take both as controllers",
			`In DS4Windows, tick Settings > "Use HidHide to Prevent Double Input", then plug the controller in again.`, ""},
	} {
		got := byID(items, id)
		if got.Text != want[0] || got.How != want[1] || got.Link != want[2] {
			t.Errorf("%s:\n got %q / %q / %q\nwant %q / %q / %q", id, got.Text, got.How, got.Link, want[0], want[1], want[2])
		}
	}

	i.Seen.DS4Windows.Answers = control.AppDSX
	if got := byID(Checklist(i), "listener"); got.Text != "DSX answers on DS4Windows' port, so DS4Windows cannot listen there" ||
		!strings.HasPrefix(got.How, "Quit DSX (its tray icon > Exit), then press Apply / Retry in DS4Windows") {
		t.Errorf("DSX on DS4Windows' port: %+v", got)
	}
	i.Seen.DS4Windows = control.Seen{Running: true, Version: "4.9.1.0", Addr: "127.0.0.1:6969"}
	if got := byID(Checklist(i), "app"); got.Text != "DS4Windows 4.9.1.0 has no game mod support" || got.Link != "ds4windows_releases" {
		t.Errorf("old DS4Windows: %+v", got)
	}
	i.Status.Kind = control.AppDSX
	if got := byID(Checklist(i), "gyro"); got.Text != "Checked once EDSense uses DS4Windows" {
		t.Errorf("on DSX: %+v", got)
	}

	d := in(control.AppDSX)
	d.Seen.DSX.Answers = ""
	if got := byID(Checklist(d), "listener"); got.Text != "DSX does not answer on 127.0.0.1:6969" ||
		got.How != "In DSX, turn on Settings > Networking > Incoming UDP." {
		t.Errorf("DSX without Incoming UDP: %+v", got)
	}
}

// TestChecker: the inputs come from the core's status, a detection, the
// session's DS4Windows checks and the HID devices, which are listed again
// only every 2 s, or after 1 s for a fresh check.
func TestChecker(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	kind := control.AppDS4Windows
	lists, rechecks := 0, 0
	var fresh []bool
	viiper := dualsense.HIDDevice{ProductID: 0x0CE6, Kind: dualsense.Virtual, Host: dualsense.HostUSBIPWin2, InLen: 64, OutLen: 48}
	phys := dualsense.HIDDevice{ProductID: 0x0CE6, Kind: dualsense.Physical, InLen: 64, OutLen: 48}
	devs := []dualsense.HIDDevice{viiper}
	c := &Checker{
		Status: func() control.Status {
			return control.Status{Full: true, Kind: kind, Online: true, Controllers: 1, Gyro: control.GyroStatus{Aim: true}}
		},
		Detect: func(f bool) control.Detection {
			fresh = append(fresh, f)
			return control.Detection{DS4Windows: control.Seen{Running: true, Answers: control.AppDS4Windows, Addr: "127.0.0.1:6969"}}
		},
		Report:  func() *ds4w.Report { return &ds4w.Report{Profile: "Elite", Output: "DualSense", Gyro: ds4w.GyroFree} },
		Recheck: func() { rechecks++ },
		List:    func() []dualsense.HIDDevice { lists++; return devs },
		Now:     func() time.Time { return now },
	}
	check := func(app string, f bool) map[string]string {
		t.Helper()
		s := c.Check(app, f)
		if s.App != app || s.T != now.UnixMilli() {
			t.Errorf("setup %s at %d", s.App, s.T)
		}
		return states(s.Items)
	}
	if got := check(control.AppDS4Windows, false); got["virtual_pad"] != "ok" || got["hidhide"] != "ok" || got["gyro"] != "ok" {
		t.Errorf("first check %v", got)
	}
	devs = []dualsense.HIDDevice{viiper, phys}
	now = now.Add(1500 * time.Millisecond)
	if got := check(control.AppDS4Windows, false); got["hidhide"] != "ok" || lists != 1 {
		t.Errorf("1.5 s later: %v, %d lists", got, lists)
	}
	if got := check(control.AppDS4Windows, true); got["hidhide"] != "bad" || lists != 2 || rechecks != 3 {
		t.Errorf("fresh: %v, %d lists, %d rechecks", got, lists, rechecks)
	}
	now = now.Add(500 * time.Millisecond)
	check(control.AppDS4Windows, true)
	if lists != 2 {
		t.Errorf("a fresh check 0.5 s after a list listed again: %d", lists)
	}
	now = now.Add(2 * time.Second)
	check(control.AppDSX, false)
	if lists != 3 {
		t.Errorf("2 s later: %d lists", lists)
	}
	if !slices.Equal(fresh, []bool{false, false, true, true, false}) {
		t.Errorf("detections %v", fresh)
	}

	// on DSX the DS4Windows checks wait, and nothing asks DS4Windows' profile again
	kind = control.AppDSX
	got := check(control.AppDS4Windows, true)
	if got["profile"] != "later" || rechecks != 4 {
		t.Errorf("on DSX: %v, %d rechecks", got, rechecks)
	}
	// DSX does not run here; the virtual pad DS4Windows made is one DSX's backend would open
	if got := check(control.AppDSX, true); got["app"] != "bad" || got["virtual_pad"] != "ok" || rechecks != 4 {
		t.Errorf("DSX: %v, %d rechecks", got, rechecks)
	}
	devs = []dualsense.HIDDevice{phys}
	now = now.Add(2 * time.Second)
	if got := check(control.AppDSX, false); got["virtual_pad"] != "wait" {
		t.Errorf("DSX without a virtual pad: %v", got)
	}
}

// TestCheckerRecheck: while EDSense uses DS4Windows, each poll has the
// session check the profile again (at most once in recheckKeep unless
// fresh) and shows that check, so an option changed in DS4Windows shows
// at the next poll.
func TestCheckerRecheck(t *testing.T) {
	defer func(w time.Duration) { reportWait = w }(reportWait)
	reportWait = 300 * time.Millisecond
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	kind := control.AppDS4Windows
	var mu sync.Mutex
	report := &ds4w.Report{Profile: "Elite", Output: "DualSense", Gyro: ds4w.GyroFree}
	rechecks, answers := 0, true
	c := &Checker{
		Status: func() control.Status {
			return control.Status{Full: true, Kind: kind, Online: true, Controllers: 1, Gyro: control.GyroStatus{Aim: true}}
		},
		Detect: func(bool) control.Detection {
			return control.Detection{DS4Windows: control.Seen{Running: true, Answers: control.AppDS4Windows, Version: "5.0.12.0"}}
		},
		Report: func() *ds4w.Report {
			mu.Lock()
			defer mu.Unlock()
			return report
		},
		Recheck: func() {
			mu.Lock()
			defer mu.Unlock()
			rechecks++
			if answers { // Trigger Lab was turned on in DS4Windows
				go func() {
					time.Sleep(30 * time.Millisecond)
					mu.Lock()
					report = &ds4w.Report{Profile: "Elite", Output: "DualSense", Gyro: ds4w.GyroFree, TriggerLab: true,
						Warnings: []ds4w.Warning{ds4w.WarnTriggerLab}}
					mu.Unlock()
				}()
			}
		},
		List: func() []dualsense.HIDDevice { return nil },
		Now:  func() time.Time { return now },
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return rechecks
	}
	if got := states(c.Check(control.AppDS4Windows, false).Items); got["trigger_lab"] != "warn" || count() != 1 {
		t.Errorf("a poll: %v, %d rechecks", got, count())
	}
	now = now.Add(time.Second)
	c.Check(control.AppDS4Windows, false)
	if count() != 1 {
		t.Errorf("a poll a second later asked again: %d", count())
	}
	c.Check(control.AppDS4Windows, true)
	if count() != 2 {
		t.Errorf("Check again did not ask: %d", count())
	}
	now = now.Add(2 * time.Second)
	c.Check(control.AppDS4Windows, false)
	if count() != 3 {
		t.Errorf("the next poll did not ask: %d", count())
	}
	// a check that brings nothing new: the last Report, after reportWait
	mu.Lock()
	answers = false
	mu.Unlock()
	now = now.Add(2 * time.Second)
	t0 := time.Now()
	got := states(c.Check(control.AppDS4Windows, false).Items)
	if d := time.Since(t0); d < reportWait || d > reportWait+time.Second || got["trigger_lab"] != "warn" {
		t.Errorf("no new check: %v after %v", got, d)
	}
	// on DSX nothing asks DS4Windows
	kind = control.AppDSX
	now = now.Add(2 * time.Second)
	c.Check(control.AppDS4Windows, true)
	if count() != 4 {
		t.Errorf("asked while on DSX: %d", count())
	}
}

func ExampleChecklist() {
	i := Inputs{App: control.AppDSX, Seen: control.Detection{DSX: control.Seen{Addr: "127.0.0.1:6969"}}}
	for _, it := range Checklist(i) {
		fmt.Printf("%s %s: %s\n", it.ID, it.State, it.Text)
	}
	// Output:
	// app bad: DSX is not running
	// listener wait: Checked once DSX runs
	// controller later: Checked once EDSense uses DSX
	// virtual_pad wait: Checked once DSX runs
	// dsx_profile unknown: Checking DSX's profiles
}

// TestChecklistFix: an item that EDSense's profile for the app fixes
// points at the profile card, while the card offers an install that does
// it.
func TestChecklistFix(t *testing.T) {
	fixes := func(items []control.Item) map[string]string {
		m := map[string]string{}
		for _, it := range items {
			if it.Fix != "" {
				m[it.ID] = it.Fix
			}
		}
		return m
	}
	d := in(control.AppDSX)
	if got := fixes(Checklist(d)); len(got) != 0 {
		t.Errorf("DSX with its profile: %v", got)
	}
	d.Profile = &control.ProfileState{App: control.AppDSX, State: control.ProfileMissing, CanInstall: true}
	if got := fixes(Checklist(d)); got["dsx_profile"] != control.FixInstall || len(got) != 1 {
		t.Errorf("DSX without its profile: %v", got)
	}
	// the player's own profile for Elite is left alone: no fix
	d.Profile = &control.ProfileState{App: control.AppDSX, State: control.ProfileNotForElite, Player: "Mine", CanReset: true}
	items := Checklist(d)
	if got := byID(items, "dsx_profile"); got.Fix != "" || got.State != control.ItemOK || got.Text != `Elite gets your DSX profile "Mine"` {
		t.Errorf("DSX's profile not for Elite: %+v", got)
	}
	d.Profile.Player = ""
	if got := byID(Checklist(d), "dsx_profile"); got.Fix != control.FixInstall || got.Text != "DSX picks no profile when Elite starts" {
		t.Errorf("no profile for Elite: %+v", got)
	}
	d.Profile.State, d.Profile.CanReset = control.ProfileNoFolder, false
	if got := fixes(Checklist(d)); len(got) != 0 {
		t.Errorf("DSX without its folder: %v", got)
	}

	steps := func(states ...string) []control.ProfileStep {
		var out []control.ProfileStep
		for i, id := range []string{ds4w.StepProfile, ds4w.StepRule, ds4w.StepListener} {
			out = append(out, control.ProfileStep{ID: id, State: states[i]})
		}
		return out
	}
	i := in(control.AppDS4Windows)
	i.Seen.DS4Windows.Answers = ""
	i.Report.Output, i.Report.Gyro = "ViiperX360", ds4w.GyroMouse
	i.Report.Warnings = []ds4w.Warning{ds4w.WarnTriggerLab, ds4w.WarnTouchpadMouse, ds4w.WarnNotDualSense}
	if got := fixes(Checklist(i)); len(got) != 0 {
		t.Errorf("no profile card: %v", got)
	}
	i.Profile = &control.ProfileState{App: control.AppDS4Windows, State: control.ProfileMissing, CanInstall: true,
		Steps: steps(ds4w.StepTodo, ds4w.StepTodo, ds4w.StepTodo)}
	want := map[string]string{"listener": "profile.install", "profile": "profile.install", "gyro": "profile.install",
		"touchpad": "profile.install", "trigger_lab": "profile.install"}
	if got := fixes(Checklist(i)); !equalMaps(got, want) {
		t.Errorf("all to do: %v", got)
	}
	// the player's own rule for Elite: only game mod support is EDSense's
	i.Profile = &control.ProfileState{App: control.AppDS4Windows, State: control.ProfileOther, CanInstall: true,
		Steps: steps(ds4w.StepSkip, ds4w.StepSkip, ds4w.StepTodo)}
	if got := fixes(Checklist(i)); !equalMaps(got, map[string]string{"listener": "profile.install"}) {
		t.Errorf("the player's rule: %v", got)
	}
	// waiting: nothing to offer
	i.Profile = &control.ProfileState{App: control.AppDS4Windows, State: control.ProfileWaiting,
		Steps: steps(ds4w.StepTodo, ds4w.StepTodo, ds4w.StepTodo)}
	if got := fixes(Checklist(i)); len(got) != 0 {
		t.Errorf("waiting: %v", got)
	}
	// ok items never point anywhere
	i = in(control.AppDS4Windows)
	i.Profile = &control.ProfileState{App: control.AppDS4Windows, State: control.ProfileMissing, CanInstall: true,
		Steps: steps(ds4w.StepTodo, ds4w.StepTodo, ds4w.StepTodo)}
	if got := fixes(Checklist(i)); len(got) != 0 {
		t.Errorf("all ok: %v", got)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestChecklistHidHide: games that still see the controller while
// DS4Windows' HidHide option is on point at HidHide itself (L12); the
// option is only a hint.
func TestChecklistHidHide(t *testing.T) {
	i := in(control.AppDS4Windows)
	i.Plan = &ds4w.Plan{Exclusive: true}
	if got := byID(Checklist(i), "hidhide"); got.State != control.ItemOK {
		t.Errorf("hidden: %+v", got)
	}
	i.Pads.Physical = true
	got := byID(Checklist(i), "hidhide")
	if got.State != control.ItemBad || !strings.Contains(got.How, "Check in HidHide that it hides the DualSense") {
		t.Errorf("visible with the option on: %+v", got)
	}
	i.Plan.Exclusive = false
	if got := byID(Checklist(i), "hidhide"); !strings.HasPrefix(got.How, `In DS4Windows, tick Settings > "Use HidHide`) {
		t.Errorf("visible with the option off: %+v", got)
	}
}
