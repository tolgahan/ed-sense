package ds4w

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every test here works in a temporary folder made from the fixtures
// below and testdata; none reads or writes a real DS4Windows folder.

const odyssey = `C:\Games\Elite Dangerous\Products\elite-dangerous-odyssey-64\EliteDangerous64.exe`

// settingsText is Profiles.xml as DS4Windows 5 saves it, with c1 as the
// first controller's profile and listener as UseDSXUDPServer's line.
func settingsText(c1, listener string) string {
	s := `<?xml version="1.0" encoding="utf-8"?>
<!-- Made for EDSense's tests, in the shape DS4Windows 5 saves -->
<Profile app_version="5.0.12.0" config_version="5">
  <useExclusiveMode>True</useExclusiveMode>
  <Controller1>` + c1 + `</Controller1>
` + listener + `  <DSXUDPServerPort>6969</DSXUDPServerPort>
  <DSXUDPServerListenAddress>127.0.0.1</DSXUDPServerListenAddress>
</Profile>
`
	return crlf(s)
}

const lineOff = "  <UseDSXUDPServer>False</UseDSXUDPServer>\n"
const lineOn = "  <UseDSXUDPServer>True</UseDSXUDPServer>\n"

// autoText is "Auto Profiles.xml" as DS4Windows saves it, with these rules.
func autoText(rules ...string) string {
	return crlf(`<?xml version="1.0" encoding="utf-8"?>
<!-- Auto-Profile Configuration Data. 01/10/2026 12:00:00 -->

<Programs>
` + strings.Join(rules, "") + `</Programs>
`)
}

// prog is a rule; profiles are Controller1 on.
func prog(path, title, attrs string, profiles ...string) string {
	s := `  <Program path="` + path + `" title="` + title + `"` + attrs + ">\n"
	for i, p := range profiles {
		n := string(rune('1' + i))
		s += "    <Controller" + n + ">" + p + "</Controller" + n + ">\n"
	}
	return s + "    <TurnOff>False</TurnOff>\n  </Program>\n"
}

// dsFolder makes a DS4Windows data folder with these files, and the
// profiles named copied from testdata.
func dsFolder(t *testing.T, settings, auto string, profiles ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "DS4Windows")
	if err := os.MkdirAll(filepath.Join(dir, profilesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, settingsFile), settings)
	write(t, filepath.Join(dir, autoProfilesFile), auto)
	for _, p := range profiles {
		b, err := os.ReadFile(filepath.Join(data, profilesDir, p+".xml"))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, profilesDir, p+".xml"), string(b))
	}
	return dir
}

func steps(p Plan) string {
	var s []string
	for _, st := range p.Steps {
		s = append(s, st.State)
	}
	return strings.Join(s, " ")
}

// TestInspect: every state, the player's rules in each shape the matcher
// knows, and what blocks an install.
func TestInspect(t *testing.T) {
	oursRule := ruleBody
	for _, c := range []struct {
		name     string
		settings string
		auto     string
		profiles []string
		ours     bool // EDSense's profile file is there
		in       Input
		state    string
		steps    string
		rule     string // the player's rule's profile, "-" for none
		loses    bool   // the player's rule loses to EDSense's for the DualSense
	}{
		{name: "missing", settings: settingsText("Default", lineOff), auto: autoText(),
			profiles: []string{"Default"}, state: PlanMissing, steps: "todo todo todo", rule: "-"},
		{name: "missing, other rules", settings: settingsText("Default", lineOff),
			auto:     autoText(prog(`^C:\Tools\`, "", ` applyToAllControllers="true"`, "Tools"), prog("", "Some window", "", "Window")),
			profiles: []string{"Default"}, state: PlanMissing, steps: "todo todo todo", rule: "-"},
		{name: "profile there", settings: settingsText("Default", lineOff), auto: autoText(), ours: true,
			state: PlanPartial, steps: "done todo todo", rule: "-"},
		{name: "listener on", settings: settingsText("Default", lineOn), auto: autoText(),
			state: PlanPartial, steps: "todo todo done", rule: "-"},
		{name: "listener with a bad port", settings: strings.Replace(settingsText("Default", lineOn), ">6969<", ">0<", 1), auto: autoText(),
			state: PlanMissing, steps: "todo todo todo", rule: "-"},
		{name: "ours", settings: settingsText("Default", lineOn), auto: autoText(oursRule), ours: true,
			state: PlanOurs, steps: "done done done", rule: "-"},
		{name: "ours, profile deleted", settings: settingsText("Default", lineOn), auto: autoText(oursRule),
			state: PlanPartial, steps: "todo done done", rule: "-"},
		{name: "ours, listener off", settings: settingsText("Default", lineOff), auto: autoText(oursRule), ours: true,
			state: PlanPartial, steps: "done done todo", rule: "-"},
		{name: "player's full path", settings: settingsText("Default", lineOff),
			auto: autoText(prog(odyssey, "", "", "Elite Mouse", "(none)")), profiles: []string{"Elite Mouse"},
			in: Input{Elite: []string{odyssey}}, state: PlanOther, steps: "skip skip todo", rule: "Elite Mouse"},
		{name: "player's ^ rule", settings: settingsText("Default", lineOff),
			auto:  autoText(prog(`^D:\SteamLibrary\steamapps\common\ED\`, "", "", "Mine")),
			in:    Input{Elite: []string{`D:\SteamLibrary\steamapps\common\ED\Products\x\EliteDangerous64.exe`}},
			state: PlanOther, steps: "skip skip todo", rule: "Mine"},
		{name: "player's $ rule", settings: settingsText("Default", lineOn),
			auto:  autoText(prog(`\products\elite-dangerous-odyssey-64\elitedangerous64.exe$`, "", "", "Mine")),
			state: PlanOther, steps: "skip skip done", rule: "Mine"},
		{name: "player's * rule", settings: settingsText("Default", lineOff),
			auto: autoText(prog(`*dangerous64.exe`, "", "", "Mine")), in: Input{Elite: []string{odyssey}},
			state: PlanOther, steps: "skip skip todo", rule: "Mine"},
		{name: "player's * rule, game not found", settings: settingsText("Default", lineOff),
			auto:  autoText(prog(`*EliteDangerous64.exe`, "", "", "Mine")),
			state: PlanOther, steps: "skip skip todo", rule: "Mine"},
		{name: "player's title rule", settings: settingsText("Default", lineOff),
			auto:  autoText(prog("", "Elite - Dangerous (CLIENT)", "", "", "Mine")),
			state: PlanOther, steps: "skip skip todo", rule: "Mine"},
		{name: "player's (none) rule", settings: settingsText("Default", lineOff),
			auto:  autoText(prog("EliteDangerous64.exe", "", "", "(none)", "(none)", "(none)")),
			state: PlanOther, steps: "skip skip todo", rule: ""},
		{name: "player's Epic path", settings: settingsText("Default", lineOff),
			auto:  autoText(prog(`C:\Program Files\Epic Games\EliteDangerous\Products\elite-dangerous-64\EliteDangerous64.exe`, "", ` device="DualSense"`, "Epic")),
			state: PlanOther, steps: "skip skip todo", rule: "Epic"},
		// DS4Windows picks the first rule for a DualSense, else the first
		// for any controller: EDSense's, made for a DualSense, beats the
		// player's for any controller in either order
		{name: "player's rule and ours", settings: settingsText("Default", lineOn), ours: true,
			auto:  autoText(oursRule, prog("EliteDangerous64.exe", "", "", "Mine")),
			state: PlanOurs, steps: "done done done", rule: "Mine", loses: true},
		{name: "player's rule before ours", settings: settingsText("Default", lineOn), ours: true,
			auto:  autoText(prog("EliteDangerous64.exe", "", "", "Mine"), oursRule),
			state: PlanOurs, steps: "done done done", rule: "Mine", loses: true},
		{name: "player's rule before ours, profile deleted", settings: settingsText("Default", lineOff),
			auto:  autoText(prog("EliteDangerous64.exe", "", "", "Mine"), oursRule),
			state: PlanPartial, steps: "todo done todo", rule: "Mine", loses: true},
		{name: "player's DualSense rule after ours", settings: settingsText("Default", lineOn), ours: true,
			auto:  autoText(oursRule, prog("EliteDangerous64.exe", "", ` device="DualSense"`, "Mine")),
			state: PlanOurs, steps: "done done done", rule: "Mine", loses: true},
		{name: "player's DualSense rule before ours", settings: settingsText("Default", lineOn), ours: true,
			auto:  autoText(prog("EliteDangerous64.exe", "", ` device="DualSense"`, "Mine"), oursRule),
			state: PlanOther, steps: "skip skip done", rule: "Mine"},
		{name: "player's DS4 rule and ours", settings: settingsText("Default", lineOn), ours: true,
			auto:  autoText(prog("EliteDangerous64.exe", "", ` device="DS4"`, "Mine"), oursRule),
			state: PlanOurs, steps: "done done done", rule: "Mine", loses: true},
		{name: "player's DS4 rule alone", settings: settingsText("Default", lineOff),
			auto:  autoText(prog("EliteDangerous64.exe", "", ` device="DS4"`, "Mine")),
			state: PlanOther, steps: "skip skip todo", rule: "Mine"},
		{name: "player's rule for any, then one for a DualSense", settings: settingsText("Default", lineOff),
			auto:  autoText(prog("EliteDangerous64.exe", "", "", "Any one"), prog(`*EliteDangerous64.exe`, "", ` device="DualSense"`, "DualSense one")),
			state: PlanOther, steps: "skip skip todo", rule: "DualSense one"},
		{name: "ours edited by the player", settings: settingsText("Default", lineOn), ours: true,
			auto:  autoText(strings.Replace(oursRule, "<Controller1>Elite Dangerous (EDSense)", "<Controller1>Mine", 1)),
			state: PlanOther, steps: "skip skip done", rule: "Mine"},
		{name: "usual works", settings: settingsText("Elite Passthru", lineOff), auto: autoText(),
			profiles: []string{"Elite Passthru"}, state: PlanUsualOK, steps: "skip skip todo", rule: "-"},
		{name: "usual works, listener on", settings: settingsText("Elite Passthru", lineOn), auto: autoText(),
			profiles: []string{"Elite Passthru"}, state: PlanUsualOK, steps: "skip skip done", rule: "-"},
		{name: "usual as DS4Windows told it", settings: settingsText("Default", lineOff), auto: autoText(),
			profiles: []string{"Default", "Elite Passthru"}, in: Input{Usual: "Elite Passthru"},
			state: PlanUsualOK, steps: "skip skip todo", rule: "-"},
		{name: "usual of slot 2", settings: settingsText("Default", lineOff), auto: autoText(),
			profiles: []string{"Default"}, in: Input{Slot: 1}, state: PlanMissing, steps: "todo todo todo", rule: "-"},
		{name: "usual with gyro mouse and Trigger Lab", settings: settingsText("Elite Mouse", lineOff), auto: autoText(),
			profiles: []string{"Elite Mouse"}, state: PlanMissing, steps: "todo todo todo", rule: "-"},
		{name: "usual uses the gyro", settings: settingsText("Elite Controls", lineOff), auto: autoText(),
			profiles: []string{"Elite Controls"}, state: PlanMissing, steps: "todo todo todo", rule: "-"},
		{name: "usual file missing", settings: settingsText("Elite Passthru", lineOff), auto: autoText(),
			state: PlanMissing, steps: "todo todo todo", rule: "-"},
	} {
		dir := dsFolder(t, c.settings, c.auto, c.profiles...)
		if c.ours {
			write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), string(ProfileFile("5.0.12.0")))
		}
		in := c.in
		in.Dir = dir
		p := Inspect(in)
		if p.State != c.state || steps(p) != c.steps {
			t.Errorf("%s: %s %q (%s %s %s), want %s %q", c.name, p.State, steps(p), p.Block, p.File, p.Why, c.state, c.steps)
			continue
		}
		switch {
		case c.rule == "-" && p.Rule != nil:
			t.Errorf("%s: a player's rule %+v", c.name, p.Rule)
		case c.rule != "-" && (p.Rule == nil || p.Rule.Profile != c.rule):
			t.Errorf("%s: player's rule %+v, want profile %q", c.name, p.Rule, c.rule)
		}
		if p.RuleLoses != c.loses {
			t.Errorf("%s: the player's rule loses: %v", c.name, p.RuleLoses)
		}
		if p.Version != "5.0.12.0" || !p.Exclusive {
			t.Errorf("%s: version %q, exclusive %v", c.name, p.Version, p.Exclusive)
		}
	}
}

// TestInspectFacts: the player's profile and the usual one are read for
// the card and the confirm.
func TestInspectFacts(t *testing.T) {
	dir := dsFolder(t, settingsText("Elite Mouse", lineOff), autoText(prog(odyssey, "", "", "Elite Controls")), "Elite Mouse", "Elite Controls")
	p := Inspect(Input{Dir: dir})
	if p.Rule == nil || p.Rule.Index != 1 || p.Rule.Path != odyssey {
		t.Fatalf("rule %+v", p.Rule)
	}
	if f := p.Player; !f.Read || f.Name != "Elite Controls" || f.Gyro != GyroOther || f.Output != "ViiperDualSenseEdge" || f.Works() {
		t.Errorf("player %+v", f)
	}
	if f := p.Usual; !f.Read || f.Gyro != GyroMouse || !f.TriggerLab || f.Touchpad != "Mouse" || f.Works() {
		t.Errorf("usual %+v", f)
	}
	if p.Endpoint != "127.0.0.1:6969" {
		t.Errorf("endpoint %s", p.Endpoint)
	}

	// a profile linked to the controller cannot be told without its address
	write(t, filepath.Join(dir, linkedProfilesFile), "<LinkedControllers><AABBCCDDEEFFMAC>Elite Passthru</AABBCCDDEEFFMAC></LinkedControllers>")
	if f := Inspect(Input{Dir: dir}).Usual; f.Read || f.Problem == "" {
		t.Errorf("linked: %+v", f)
	}

	// L17: the listener keeps DS4Windows' own port when it is valid
	dir = dsFolder(t, strings.Replace(settingsText("Default", lineOff), ">6969<", ">7000<", 1), autoText())
	if p := Inspect(Input{Dir: dir}); p.Endpoint != "127.0.0.1:7000" {
		t.Errorf("own port: %s", p.Endpoint)
	}
	dir = dsFolder(t, strings.Replace(settingsText("Default", lineOff), ">127.0.0.1<", ">10.0.0.2<", 1), autoText())
	if p := Inspect(Input{Dir: dir}); p.Endpoint != "127.0.0.1:6969" || p.Step(StepListener) != StepTodo {
		t.Errorf("remote address: %s %s", p.Endpoint, p.Step(StepListener))
	}
	if (Facts{}).Works() {
		t.Error("an unread profile works")
	}
}

// TestInspectBlocked: what makes EDSense leave DS4Windows' files alone.
func TestInspectBlocked(t *testing.T) {
	good := settingsText("Default", lineOff)
	for _, c := range []struct {
		name  string
		edit  func(t *testing.T, dir string) Input
		block string
		file  string
	}{
		{"no folder", func(t *testing.T, dir string) Input { return Input{} }, BlockFolder, ""},
		{"folder missing", func(t *testing.T, dir string) Input { return Input{Dir: filepath.Join(dir, "none")} }, BlockFolder, settingsFile},
		{"no Profiles.xml", func(t *testing.T, dir string) Input {
			os.Remove(filepath.Join(dir, settingsFile))
			return Input{Dir: dir}
		}, BlockFolder, settingsFile},
		{"no Auto Profiles.xml", func(t *testing.T, dir string) Input {
			os.Remove(filepath.Join(dir, autoProfilesFile))
			return Input{Dir: dir}
		}, BlockFolder, autoProfilesFile},
		{"no Profiles folder", func(t *testing.T, dir string) Input {
			os.RemoveAll(filepath.Join(dir, profilesDir))
			return Input{Dir: dir}
		}, BlockFolder, profilesDir},
		{"Profiles is a file", func(t *testing.T, dir string) Input {
			os.RemoveAll(filepath.Join(dir, profilesDir))
			write(t, filepath.Join(dir, profilesDir), "x")
			return Input{Dir: dir}
		}, BlockFolder, profilesDir},
		{"version 3", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, settingsFile), strings.Replace(good, "5.0.12.0", "3.3.3", 1))
			return Input{Dir: dir}
		}, BlockOld, ""},
		{"version 4 from the exe", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, settingsFile), strings.Replace(good, ` app_version="5.0.12.0"`, "", 1))
			return Input{Dir: dir, Version: "4.1.0.0"}
		}, BlockOld, ""},
		{"version unknown, no game mod setting", func(t *testing.T, dir string) Input {
			s := strings.Replace(good, ` app_version="5.0.12.0"`, "", 1)
			write(t, filepath.Join(dir, settingsFile), strings.Replace(s, crlf(lineOff), "", 1))
			return Input{Dir: dir}
		}, BlockOld, ""},
		{"portable lab", func(t *testing.T, dir string) Input {
			lab := filepath.Join(filepath.Dir(dir), labData)
			os.Rename(dir, lab)
			return Input{Dir: lab}
		}, BlockLab, ""},
		{"two settings folders", func(t *testing.T, dir string) Input {
			return Input{Dir: dir, ExeDir: dir, Also: t.TempDir()}
		}, BlockTwo, ""},
		{"portable lab next to the exe", func(t *testing.T, dir string) Input {
			exe := t.TempDir()
			write(t, filepath.Join(exe, labData, autoProfilesFile), "<Programs/>")
			return Input{Dir: dir, ExeDir: exe}
		}, BlockLab, ""},
		{"Profiles.xml in UTF-16", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, settingsFile), string(utf16LE(good)))
			return Input{Dir: dir}
		}, BlockUnreadable, settingsFile},
		{"Auto Profiles.xml in UTF-16", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, autoProfilesFile), string(utf16LE(autoText())))
			return Input{Dir: dir}
		}, BlockUnreadable, autoProfilesFile},
		{"Profiles.xml broken", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, settingsFile), strings.Replace(good, "</Profile>", "</Profil>", 1))
			return Input{Dir: dir}
		}, BlockUnreadable, settingsFile},
		{"Profiles.xml with another root", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, settingsFile), "<Settings app_version=\"5.0.12.0\"><UseDSXUDPServer>False</UseDSXUDPServer></Settings>")
			return Input{Dir: dir}
		}, BlockUnreadable, settingsFile},
		{"a rule with applyToAllControllers=True", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, autoProfilesFile), autoText(prog(`C:\Tools\a.exe`, "", ` applyToAllControllers="True"`, "Tools")))
			return Input{Dir: dir}
		}, BlockUnreadable, autoProfilesFile},
		{"a rule with device=dualsense", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, autoProfilesFile), autoText(prog(`C:\Tools\a.exe`, "", ` device="dualsense"`, "Tools")))
			return Input{Dir: dir}
		}, BlockUnreadable, autoProfilesFile},
		{"Auto Profiles.xml with two roots", func(t *testing.T, dir string) Input {
			write(t, filepath.Join(dir, autoProfilesFile), "<Programs/><Programs/>")
			return Input{Dir: dir}
		}, BlockUnreadable, autoProfilesFile},
	} {
		dir := dsFolder(t, good, autoText())
		in := c.edit(t, dir)
		p := Inspect(in)
		if p.State != PlanBlocked || p.Block != c.block || p.File != c.file || len(p.Steps) != 0 || p.CanInstall() || p.CanReset() {
			t.Errorf("%s: %s %s %q (%s), want blocked %s %q", c.name, p.State, p.Block, p.File, p.Why, c.block, c.file)
		}
		if c.block == BlockUnreadable && p.Why == "" {
			t.Errorf("%s: no reason", c.name)
		}
		if strings.Contains(p.Why, dir) {
			t.Errorf("%s: a full path in %q", c.name, p.Why)
		}
		msg := (&BlockedError{p}).Error()
		if msg == "" || strings.Contains(msg, dir) {
			t.Errorf("%s: message %q", c.name, msg)
		}
	}
	p := Inspect(Input{Dir: dsFolder(t, strings.Replace(good, "5.0.12.0", "3.3.3", 1), autoText())})
	if msg := (&BlockedError{p}).Error(); msg != "DS4Windows 3.3.3 has no game mod support" {
		t.Errorf("old: %q", msg)
	}
	dir := dsFolder(t, string(utf16LE(good)), autoText())
	if msg := (&BlockedError{Inspect(Input{Dir: dir})}).Error(); msg != "Profiles.xml could not be read (it is saved as UTF-16)" {
		t.Errorf("UTF-16: %q", msg)
	}
	// a version that is not known, with the game mod setting there, is not old
	dir = dsFolder(t, strings.Replace(good, ` app_version="5.0.12.0"`, "", 1), autoText())
	if p := Inspect(Input{Dir: dir}); p.State != PlanMissing || p.Version != "" {
		t.Errorf("version not known: %s %q", p.State, p.Version)
	}
}

// snapshot is every file under dir, by its path from dir.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

var at = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newWriter(t *testing.T, closed func() (bool, string)) writer {
	if closed == nil {
		closed = func() (bool, string) { return true, "" }
	}
	return writer{backups: filepath.Join(t.TempDir(), "ds4windows_backups"), closed: closed, now: at, put: put}
}

// TestInstall: the three files are made, copied first, and written in
// order; the rest of each stays byte for byte.
func TestInstall(t *testing.T) {
	settings, auto := settingsText("Default", lineOff), autoText(prog(`C:\Tools\a.exe`, "", "", "Tools"))
	dir := dsFolder(t, settings, auto, "Default")
	w := newWriter(t, nil)
	res, err := w.install(Input{Dir: dir}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Wrote, []string{StepProfile, StepRule, StepListener}) || res.Restored || res.Dir != dir {
		t.Fatalf("result %+v", res)
	}
	if res.Backup != filepath.Join(w.backups, "20261001-120000") {
		t.Errorf("backup folder %s", res.Backup)
	}
	got := snapshot(t, dir)
	wantRule, _ := SpliceRule([]byte(auto))
	wantSettings, _ := SetListener([]byte(settings))
	if got[profPath("Elite Dangerous (EDSense)")] != string(ProfileFile("5.0.12.0")) ||
		got[autoProfilesFile] != string(wantRule) || got[settingsFile] != string(wantSettings) || len(got) != 4 {
		t.Errorf("files %v", keys(got))
	}
	if back := snapshot(t, res.Backup); len(back) != 2 || back[settingsFile] != settings || back[autoProfilesFile] != auto {
		t.Errorf("copies %v", keys(back))
	}
	if p := Inspect(Input{Dir: dir}); p.State != PlanOurs || p.CanInstall() || !p.CanReset() {
		t.Errorf("after: %s %s", p.State, steps(p))
	}
	if did := res.Did(); !strings.Contains(did, `"Elite Dangerous (EDSense)"`) || !strings.Contains(did, "Auto Profiles rule") ||
		!strings.Contains(did, "game mod support on") || !strings.Contains(did, res.Backup) {
		t.Errorf("did: %s", did)
	}

	// nothing left: nothing written, no copies
	res, err = w.install(Input{Dir: dir}, false, nil)
	if err != nil || len(res.Wrote) != 0 || res.Backup != "" {
		t.Errorf("again: %+v %v", res, err)
	}

	// reset: only the profile, the player's changes to it kept in the copy
	mine := strings.Replace(string(ProfileFile("5.0.12.0")), "0,0,255", "255,0,0", 1)
	write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), mine)
	before := snapshot(t, dir)
	res, err = w.install(Input{Dir: dir}, true, nil)
	if err != nil || !slices.Equal(res.Wrote, []string{StepProfile}) {
		t.Fatalf("reset: %+v %v", res, err)
	}
	if res.Backup != filepath.Join(w.backups, "20261001-120000-2") {
		t.Errorf("second backup in the same second: %s", res.Backup)
	}
	after := snapshot(t, dir)
	if after[profPath(ProfileName)] != string(ProfileFile("5.0.12.0")) || after[autoProfilesFile] != before[autoProfilesFile] || after[settingsFile] != before[settingsFile] {
		t.Error("reset wrote more than the profile")
	}
	if back := snapshot(t, res.Backup); back[ProfileName+".xml"] != mine || len(back) != 3 {
		t.Errorf("reset copies %v", keys(back))
	}
}

// profPath is a profile's path in a snapshot.
func profPath(name string) string { return profilesDir + "/" + name + ".xml" }

func keys(m map[string]string) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	slices.Sort(k)
	return k
}

// TestInstallSteps: only the steps to do are written: the listener alone
// beside the player's rule, nothing of a blocked plan.
func TestInstallSteps(t *testing.T) {
	settings := settingsText("Default", lineOff)
	auto := autoText(prog("EliteDangerous64.exe", "", "", "Mine"))
	dir := dsFolder(t, settings, auto, "Default")
	res, err := newWriter(t, nil).install(Input{Dir: dir}, false, nil)
	if err != nil || !slices.Equal(res.Wrote, []string{StepListener}) {
		t.Fatalf("other: %+v %v", res, err)
	}
	got := snapshot(t, dir)
	if got[autoProfilesFile] != auto || got[profPath(ProfileName)] != "" {
		t.Error("the player's rule was touched")
	}
	if _, err := newWriter(t, nil).install(Input{Dir: dir}, true, nil); err != nil {
		t.Errorf("reset beside the player's rule: %v", err)
	}

	dir = dsFolder(t, settings, autoText(prog("a.exe", "", ` applyToAllControllers="True"`, "X")))
	before := snapshot(t, dir)
	w := newWriter(t, nil)
	_, err = w.install(Input{Dir: dir}, false, nil)
	var be *BlockedError
	if !errors.As(err, &be) || be.Plan.Block != BlockUnreadable {
		t.Errorf("blocked: %v", err)
	}
	if !equalMaps(snapshot(t, dir), before) || exists(w.backups) {
		t.Error("a blocked plan wrote something")
	}
}

// TestInstallAgreed: the install writes none but the steps the player
// agreed to; a plan that needs another one now writes nothing, not even
// the copies; one that needs fewer writes those.
func TestInstallAgreed(t *testing.T) {
	// only the listener was agreed to (the profile and rule were EDSense's
	// already), then the player deleted the rule before Exit
	settings := settingsText("Default", lineOff)
	dir := dsFolder(t, settings, autoText())
	write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), string(ProfileFile("5.0.12.0")))
	before := snapshot(t, dir)
	w := newWriter(t, nil)
	_, err := w.install(Input{Dir: dir}, false, []string{StepListener})
	var ch *ChangedError
	if !errors.As(err, &ch) || ch.Step != StepRule || !strings.Contains(err.Error(), "Auto Profiles.xml") {
		t.Fatalf("grown: %v", err)
	}
	if !equalMaps(snapshot(t, dir), before) || exists(w.backups) {
		t.Error("grown: written")
	}

	// fewer: the player turned game mod support on before Exit
	write(t, filepath.Join(dir, settingsFile), settingsText("Default", lineOn))
	res, err := w.install(Input{Dir: dir}, false, []string{StepProfile, StepRule, StepListener})
	if err != nil || !slices.Equal(res.Wrote, []string{StepRule}) {
		t.Errorf("fewer: %+v %v", res, err)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// TestInstallClosed: while DS4Windows runs nothing is written, not even
// the copies; when it starts during the writes, the install stops and
// says what it wrote.
func TestInstallClosed(t *testing.T) {
	settings, auto := settingsText("Default", lineOff), autoText()
	dir := dsFolder(t, settings, auto)
	before := snapshot(t, dir)
	w := newWriter(t, func() (bool, string) { return false, "DS4Windows.exe runs" })
	_, err := w.install(Input{Dir: dir}, false, nil)
	var nc notClosed
	if !errors.As(err, &nc) || !equalMaps(snapshot(t, dir), before) || exists(w.backups) {
		t.Fatalf("running: %v", err)
	}

	// DS4Windows starts after the copies, before the first write
	calls := 0
	w = newWriter(t, func() (bool, string) { calls++; return calls < 2, "it runs" })
	_, err = w.install(Input{Dir: dir}, false, nil)
	if !errors.As(err, &nc) || !equalMaps(snapshot(t, dir), before) {
		t.Fatalf("before the first write: %v", err)
	}

	// and after the profile
	calls = 0
	w = newWriter(t, func() (bool, string) { calls++; return calls < 3, "it runs" })
	res, err := w.install(Input{Dir: dir}, false, nil)
	var stop *InterruptedError
	if !errors.As(err, &stop) || !slices.Equal(stop.Wrote, []string{StepProfile}) || !slices.Equal(res.Wrote, []string{StepProfile}) {
		t.Fatalf("mid-write: %+v %v", res, err)
	}
	got := snapshot(t, dir)
	if got[autoProfilesFile] != auto || got[settingsFile] != settings || got[profPath(ProfileName)] == "" {
		t.Error("mid-write: the files after the stop were written")
	}
	if p := Inspect(Input{Dir: dir}); p.State != PlanPartial || steps(p) != "done todo todo" {
		t.Errorf("after the stop: %s %s", p.State, steps(p))
	}

	// a file that changed after Inspect read it counts as DS4Windows
	// having run: the install stops there
	dir = dsFolder(t, settings, auto)
	calls = 0
	w = newWriter(t, func() (bool, string) {
		calls++
		if calls == 3 {
			write(t, filepath.Join(dir, autoProfilesFile), autoText(prog("x.exe", "", "", "X")))
		}
		return true, ""
	})
	res, err = w.install(Input{Dir: dir}, false, nil)
	if !errors.As(err, &stop) || !strings.Contains(stop.Why, "Auto Profiles.xml changed") || !slices.Equal(res.Wrote, []string{StepProfile}) {
		t.Errorf("changed: %+v %v", res, err)
	}
	if got := snapshot(t, dir); got[settingsFile] != settings {
		t.Error("changed: wrote on")
	}
}

// TestInstallRestores: a file that does not read back as written is put
// back from the copy, or removed when it is new; nothing after it is
// written.
func TestInstallRestores(t *testing.T) {
	settings, auto := settingsText("Default", lineOff), autoText()
	for _, c := range []struct {
		name  string
		bad   string // the file put writes wrong
		wrote []string
	}{
		{"profile", profPath(ProfileName), nil},
		{"rule", autoProfilesFile, []string{StepProfile}},
		{"listener", settingsFile, []string{StepProfile, StepRule}},
	} {
		dir := dsFolder(t, settings, auto)
		before := snapshot(t, dir)
		w := newWriter(t, nil)
		w.put = func(path string, b []byte) error {
			if strings.HasSuffix(filepath.ToSlash(path), "/"+c.bad) {
				b = b[:len(b)/2] // a write cut short
			}
			return put(path, b)
		}
		res, err := w.install(Input{Dir: dir}, false, nil)
		if err == nil || !res.Restored || !slices.Equal(res.Wrote, c.wrote) {
			t.Errorf("%s: %+v %v", c.name, res, err)
			continue
		}
		got := snapshot(t, dir)
		if got[c.bad] != before[c.bad] {
			t.Errorf("%s: not put back", c.name)
		}
		if _, there := got[c.bad]; there != (c.bad != profPath(ProfileName)) {
			t.Errorf("%s: the new profile was not removed", c.name)
		}
		// the files after the one that failed stay as they were
		if c.bad != settingsFile && got[settingsFile] != before[settingsFile] {
			t.Errorf("%s: Profiles.xml written after the failure", c.name)
		}
		if c.bad == profPath(ProfileName) && got[autoProfilesFile] != before[autoProfilesFile] {
			t.Errorf("%s: Auto Profiles.xml written after the failure", c.name)
		}
		if strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "so it was put back from its copy") {
			t.Errorf("%s: %q", c.name, err)
		}
		for name := range got {
			if strings.HasSuffix(name, ".tmp") {
				t.Errorf("%s: %s left", c.name, name)
			}
		}
	}

	// a write that fails leaves the file as it was
	dir := dsFolder(t, settings, auto)
	w := newWriter(t, nil)
	w.put = func(path string, b []byte) error {
		if filepath.Base(path) == settingsFile {
			return errors.New("denied")
		}
		return put(path, b)
	}
	res, err := w.install(Input{Dir: dir}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "denied") || snapshot(t, dir)[settingsFile] != settings || len(res.Wrote) != 2 {
		t.Errorf("failed write: %+v %v", res, err)
	}
	// it was never replaced: nothing was put back, and the error says so
	if res.Restored || !strings.Contains(err.Error(), "Profiles.xml could not be written") || !strings.Contains(err.Error(), "left as it was") {
		t.Errorf("failed write: restored %v, %v", res.Restored, err)
	}

	// an error with a path names only the file
	if got := plainErr(&os.PathError{Op: "open", Path: filepath.Join(dir, "x.tmp"), Err: errors.New("denied")}); got != "open x.tmp: denied" {
		t.Errorf("path error: %s", got)
	}
	if got := plainErr(&os.LinkError{Op: "rename", Old: filepath.Join(dir, "a.tmp"), New: filepath.Join(dir, settingsFile), Err: errors.New("denied")}); got != "rename a.tmp Profiles.xml: denied" {
		t.Errorf("link error: %s", got)
	}
}

// TestInstallBackupFails: without the copies nothing is written.
func TestInstallBackupFails(t *testing.T) {
	dir := dsFolder(t, settingsText("Default", lineOff), autoText())
	before := snapshot(t, dir)
	w := newWriter(t, nil)
	write(t, w.backups, "a file where the folder goes")
	if _, err := w.install(Input{Dir: dir}, false, nil); err == nil || !strings.Contains(err.Error(), "nothing was written") ||
		strings.Contains(err.Error(), filepath.Dir(w.backups)) {
		t.Errorf("backup into a file: %v", err)
	}
	w.backups = ""
	if _, err := w.install(Input{Dir: dir}, false, nil); err == nil {
		t.Error("no backup folder")
	}
	if !equalMaps(snapshot(t, dir), before) {
		t.Error("written without copies")
	}
}

// fakeClosed is DS4Windows' closed check as a test scripts it.
type fakeClosed struct {
	mu     sync.Mutex
	closed bool
	calls  int
	exe    []string // the exe folders asked about
	hook   func(call int) bool
}

func (f *fakeClosed) check(exeDir string) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.exe = append(f.exe, exeDir)
	if f.hook != nil {
		return f.hook(f.calls), "scripted"
	}
	return f.closed, "it runs"
}

func (f *fakeClosed) set(closed bool) {
	f.mu.Lock()
	f.closed, f.hook = closed, nil
	f.mu.Unlock()
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newInstaller(t *testing.T) (*Installer, *fakeClosed, *fakeClock, *lockedWriter) {
	t.Helper()
	var logged lockedWriter
	old, flags := log.Writer(), log.Flags()
	log.SetOutput(&logged)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(old); log.SetFlags(flags) })
	f, clock := &fakeClosed{}, &fakeClock{t: at}
	i := NewInstaller(InstallerOptions{Backups: filepath.Join(t.TempDir(), "ds4windows_backups"), Closed: f.check, Now: clock.now})
	return i, f, clock, &logged
}

// TestInstaller: a request waits until DS4Windows has been closed for
// 2 s, says once that it waits, then writes and logs what it did.
func TestInstaller(t *testing.T) {
	i, f, clock, logged := newInstaller(t)
	dir := dsFolder(t, settingsText("Default", lineOff), autoText())
	before := snapshot(t, dir)
	in := Input{Dir: dir, ExeDir: `C:\Tools\DS4Windows`}
	if j := i.Job(); j.State != "" {
		t.Fatalf("job before a request: %+v", j)
	}
	if err := i.Request(in, false); err != nil {
		t.Fatal(err)
	}
	if j := i.Job(); j.State != JobWaiting || j.Dir != dir || j.Reset {
		t.Fatalf("requested: %+v", j)
	}
	for range 3 {
		i.Step()
		clock.add(time.Second)
	}
	if !equalMaps(snapshot(t, dir), before) || i.Job().State != JobWaiting {
		t.Fatal("written while DS4Windows ran")
	}
	if n := strings.Count(logged.String(), "DS4Windows profile: waiting for DS4Windows to be closed\n"); n != 1 {
		t.Errorf("waiting line %d times:\n%s", n, logged)
	}
	if f.exe[0] != in.ExeDir {
		t.Errorf("closed asked about %q", f.exe[0])
	}

	// closed for 1.5 s, then running again: the hold starts over
	f.set(true)
	i.Step()
	clock.add(1500 * time.Millisecond)
	i.Step()
	f.set(false)
	clock.add(time.Second)
	i.Step()
	f.set(true)
	i.Step()
	clock.add(1900 * time.Millisecond)
	i.Step()
	if !equalMaps(snapshot(t, dir), before) {
		t.Fatal("written before 2 s closed in a row")
	}
	clock.add(100 * time.Millisecond)
	i.Step()
	j := i.Job()
	if j.State != JobDone || len(j.Result.Wrote) != 3 || j.Err != "" {
		t.Fatalf("done: %+v", j)
	}
	if p := Inspect(Input{Dir: dir}); p.State != PlanOurs {
		t.Errorf("after: %s", p.State)
	}
	if !strings.Contains(logged.String(), "DS4Windows profile: wrote ") || strings.Count(logged.String(), "waiting for") != 1 {
		t.Errorf("log:\n%s", logged)
	}
	i.Step() // done: nothing more
	if i.Job().State != JobDone {
		t.Error("a done job changed")
	}

	// nothing left to do, a reset of the profile
	if err := i.Request(Input{Dir: dir}, false); !errors.Is(err, ErrNothing) {
		t.Errorf("nothing to do: %v", err)
	}
	if err := i.Request(Input{Dir: dir}, true); err != nil {
		t.Fatal(err)
	}
	if j := i.Job(); j.State != JobWaiting || !j.Reset || len(j.Result.Wrote) != 0 {
		t.Errorf("reset requested: %+v", j)
	}
	i.Step()
	clock.add(2 * time.Second)
	i.Step()
	if j := i.Job(); j.State != JobDone || !slices.Equal(j.Result.Wrote, []string{StepProfile}) {
		t.Errorf("reset: %+v", j)
	}
}

// TestInstallerAgreed: a job keeps the steps it was asked for; one that
// would write another once DS4Windows is closed fails and writes nothing;
// a request for steps that are not the ones to do now is refused.
func TestInstallerAgreed(t *testing.T) {
	i, f, clock, _ := newInstaller(t)
	dir := dsFolder(t, settingsText("Default", lineOff), autoText(ruleBody))
	write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), string(ProfileFile("5.0.12.0")))
	if err := i.RequestSteps(Input{Dir: dir}, false, []string{StepProfile, StepRule, StepListener}); !errors.Is(err, ErrChanged) {
		t.Errorf("other steps shown: %v", err)
	}
	if err := i.RequestSteps(Input{Dir: dir}, false, []string{StepListener}); err != nil {
		t.Fatal(err)
	}
	if j := i.Job(); !slices.Equal(j.Steps, []string{StepListener}) {
		t.Fatalf("steps %q", j.Steps)
	}
	// the player deletes EDSense's rule, then exits DS4Windows
	write(t, filepath.Join(dir, autoProfilesFile), autoText())
	before := snapshot(t, dir)
	f.set(true)
	i.Step()
	clock.add(2 * time.Second)
	i.Step()
	j := i.Job()
	if j.State != JobFailed || !strings.Contains(j.Err, "nothing was written") || len(j.Result.Wrote) != 0 {
		t.Fatalf("grown: %+v", j)
	}
	if !equalMaps(snapshot(t, dir), before) || exists(i.o.Backups) {
		t.Error("grown: written")
	}
	// Request takes the steps to do now
	if err := i.Request(Input{Dir: dir}, false); err != nil || !slices.Equal(i.Job().Steps, []string{StepRule, StepListener}) {
		t.Errorf("request: %+v %v", i.Job(), err)
	}
}

// TestInstallerCancel: a waiting install can be cancelled or replaced;
// a blocked plan is refused at once.
func TestInstallerCancel(t *testing.T) {
	i, f, clock, _ := newInstaller(t)
	dir := dsFolder(t, settingsText("Default", lineOff), autoText())
	before := snapshot(t, dir)
	if i.Cancel() {
		t.Error("cancelled nothing")
	}
	if err := i.Request(Input{Dir: dir}, false); err != nil {
		t.Fatal(err)
	}
	if !i.Cancel() || i.Job().State != "" {
		t.Fatal("not cancelled")
	}
	f.set(true)
	for range 4 {
		i.Step()
		clock.add(time.Second)
	}
	if !equalMaps(snapshot(t, dir), before) {
		t.Error("a cancelled install wrote")
	}

	other := dsFolder(t, settingsText("Default", lineOff), autoText())
	i.Request(Input{Dir: dir}, false)
	if err := i.Request(Input{Dir: other}, false); err != nil || i.Job().Dir != other {
		t.Errorf("replaced: %+v %v", i.Job(), err)
	}

	bad := dsFolder(t, string(utf16LE(settingsText("Default", lineOff))), autoText())
	var be *BlockedError
	if err := i.Request(Input{Dir: bad}, false); !errors.As(err, &be) || i.Job().Dir != other {
		t.Errorf("blocked: %v", err)
	}
	if err := i.Request(Input{}, false); !errors.As(err, &be) {
		t.Errorf("no folder: %v", err)
	}
}

// TestInstallerOutcomes: a stop mid-write and a failure are kept with
// their reasons and logged; a request while writing is refused.
func TestInstallerOutcomes(t *testing.T) {
	i, f, clock, logged := newInstaller(t)
	dir := dsFolder(t, settingsText("Default", lineOff), autoText())
	i.Request(Input{Dir: dir}, false)
	// closed for the hold and the first write, then DS4Windows starts
	f.hook = func(call int) bool { return call <= 4 }
	i.Step()
	clock.add(2 * time.Second)
	i.Step()
	j := i.Job()
	if j.State != JobInterrupted || !slices.Equal(j.Result.Wrote, []string{StepProfile}) || !strings.Contains(j.Err, "started while EDSense was writing") {
		t.Fatalf("interrupted: %+v", j)
	}
	if !strings.Contains(logged.String(), "DS4Windows profile not written: DS4Windows started") {
		t.Errorf("log:\n%s", logged)
	}

	// a failure, put back
	f.set(true)
	i.put = func(path string, b []byte) error {
		if filepath.Base(path) == autoProfilesFile {
			b = []byte("<Programs>")
		}
		return put(path, b)
	}
	if err := i.Request(Input{Dir: dir}, false); err != nil {
		t.Fatal(err)
	}
	i.Step()
	clock.add(2 * time.Second)
	i.Step()
	j = i.Job()
	if j.State != JobFailed || !j.Result.Restored || !strings.Contains(j.Err, "Auto Profiles.xml was not written right") {
		t.Fatalf("failed: %+v", j)
	}
	if !strings.Contains(logged.String(), "DS4Windows profile not written: Auto Profiles.xml") {
		t.Errorf("log:\n%s", logged)
	}

	// busy while writing
	i.put = put
	release, entered := make(chan struct{}), make(chan struct{})
	i.put = func(path string, b []byte) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return put(path, b)
	}
	if err := i.Request(Input{Dir: dir}, false); err != nil {
		t.Fatal(err)
	}
	i.Step()
	clock.add(2 * time.Second)
	done := make(chan struct{})
	go func() { i.Step(); close(done) }()
	<-entered
	if err := i.Request(Input{Dir: dir}, false); !errors.Is(err, ErrBusy) {
		t.Errorf("while writing: %v", err)
	}
	if i.Cancel() {
		t.Error("cancelled while writing")
	}
	if j := i.Job(); j.State != JobWaiting || !j.Writing {
		t.Errorf("while writing: %+v", j)
	}
	close(release)
	<-done
	if j := i.Job(); j.State != JobDone || j.Writing {
		t.Errorf("after writing: %+v", j)
	}

	// a bug while writing fails the job; the Installer goes on
	i.put = func(string, []byte) error { panic("a bug") }
	if err := i.Request(Input{Dir: dir}, true); err != nil {
		t.Fatal(err)
	}
	i.Step()
	clock.add(2 * time.Second)
	i.Step()
	if j := i.Job(); j.State != JobFailed || j.Writing || !strings.Contains(j.Err, "a bug") {
		t.Errorf("panic: %+v", j)
	}
	i.put = put
	if err := i.Request(Input{Dir: dir}, true); err != nil {
		t.Errorf("after a panic: %v", err)
	}
}

// TestCustomExe: the name in custom_exe_name.txt, as DS4Windows reads it.
func TestCustomExe(t *testing.T) {
	read := func(s string, err error) func(string) ([]byte, error) {
		return func(string) ([]byte, error) { return []byte(s), err }
	}
	for _, c := range []struct {
		text string
		err  error
		want string
	}{
		{"MyMapper", nil, "MyMapper.exe"},
		{" MyMapper \r\n", nil, "MyMapper.exe"},
		{"\ufeffMyMapper", nil, "MyMapper.exe"},
		{"", nil, ""},
		{`..\x`, nil, ""},
		{"a:b", nil, ""},
		{"x", os.ErrNotExist, ""},
	} {
		if got := customExe(`C:\DS4Windows`, read(c.text, c.err)); got != c.want {
			t.Errorf("%q: %q, want %q", c.text, got, c.want)
		}
	}
	if got := customExe("", read("x", nil)); got != "" {
		t.Errorf("no folder: %q", got)
	}
}

// TestPlanFiles: the files an install writes, named for the window, and
// the data folder without the player's user name (L14).
func TestPlanFiles(t *testing.T) {
	dir := dsFolder(t, settingsText("Default", lineOff), autoText())
	p := Inspect(Input{Dir: dir})
	want := []string{profilesDir + `\` + ProfileName + ".xml", autoProfilesFile, settingsFile}
	if got := p.Files(false); !slices.Equal(got, want) {
		t.Errorf("files %q", got)
	}
	if got := p.Files(true); !slices.Equal(got, want[:1]) {
		t.Errorf("reset files %q", got)
	}
	if StepFile("x") != "" {
		t.Error("a file for no step")
	}

	home := filepath.Join(t.TempDir(), "Users", "Player")
	appData := filepath.Join(home, "AppData", "Roaming")
	for _, c := range []struct{ dir, want string }{
		{filepath.Join(appData, "DS4Windows"), `%APPDATA%\DS4Windows`},
		{appData, `%APPDATA%`},
		{filepath.Join(home, "Tools", "DS4Windows"), `%USERPROFILE%\Tools\DS4Windows`},
		{filepath.Join(filepath.Dir(home), "Other", "DS4Windows"), filepath.Join(filepath.Dir(home), "Other", "DS4Windows")},
		{filepath.Join(home+"2", "DS4Windows"), filepath.Join(home+"2", "DS4Windows")},
	} {
		if got := ShowDir(c.dir, appData, home); got != c.want {
			t.Errorf("%s: %s, want %s", c.dir, got, c.want)
		}
	}
	if got := ShowDir(filepath.Join(appData, "DS4Windows"), "", ""); got != filepath.Join(appData, "DS4Windows") {
		t.Errorf("no roots: %s", got)
	}

	r := Result{Dir: "D", Backup: "B", Wrote: []string{StepProfile, StepRule, StepListener}}
	if got := r.Did(); got != `wrote the profile "Elite Dangerous (EDSense)", added the Auto Profiles rule for Elite and turned game mod support on, in D; copies of the old files are in B` {
		t.Errorf("did: %s", got)
	}
	r.Wrote = []string{StepListener}
	if got := r.Did(); got != "turned game mod support on, in D; copies of the old files are in B" {
		t.Errorf("did: %s", got)
	}
}
