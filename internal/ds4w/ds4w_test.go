package ds4w

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// data is a DS4Windows data folder in the shape DS4Windows 5 saves it.
var data = filepath.Join("testdata", "DS4Windows")

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDataDir: a portable DS4Windows keeps "Auto Profiles.xml" next to its
// exe; else its settings are in %APPDATA%\DS4Windows.
func TestDataDir(t *testing.T) {
	portable, appData := t.TempDir(), t.TempDir()
	if got := DataDir(portable, appData); got != filepath.Join(appData, "DS4Windows") {
		t.Errorf("installed: %s", got)
	}
	write(t, filepath.Join(portable, "Auto Profiles.xml"), "<Programs/>")
	if got := DataDir(portable, appData); got != portable {
		t.Errorf("portable: %s", got)
	}
	if got := DataDir("", appData); got != filepath.Join(appData, "DS4Windows") {
		t.Errorf("not running: %s", got)
	}
	if got := DataDir("", ""); got != "" {
		t.Errorf("nothing known: %q", got)
	}
}

func TestReadSettings(t *testing.T) {
	s, err := ReadSettings(data)
	if err != nil {
		t.Fatal(err)
	}
	if s.AppVersion != "5.0.12.0" || s.Controllers[0] != "Elite Passthru" || s.Controllers[1] != "Default" || s.Controllers[2] != "" || !s.Listener || s.Port != 6970 {
		t.Fatalf("settings %+v", s)
	}
	if got := s.Endpoint(0).String(); got != "127.0.0.1:6970" {
		t.Errorf("endpoint %s", got)
	}
	if got := s.Endpoint(7000).String(); got != "127.0.0.1:7000" {
		t.Errorf("ds4windows_port: %s", got)
	}
	for _, c := range []struct {
		s    Settings
		want string
	}{
		{Settings{Port: 6969, Address: "::1"}, "[::1]:6969"},
		{Settings{Port: 6969, Address: "192.168.1.5"}, "127.0.0.1:6969"}, // DS4Windows listens on loopback only
		{Settings{Port: 0, Address: "127.0.0.1"}, "127.0.0.1:6969"},
		{Settings{}, "127.0.0.1:6969"},
	} {
		if got := c.s.Endpoint(0).String(); got != c.want {
			t.Errorf("%+v: %s, want %s", c.s, got, c.want)
		}
	}
	if _, err := ReadSettings(t.TempDir()); err == nil {
		t.Error("a folder without Profiles.xml read")
	}
	if n, ok := Major("5.0.12.0"); n != 5 || !ok {
		t.Errorf("major %d %v", n, ok)
	}
	if _, ok := Major(""); ok {
		t.Error("no version has a major")
	}
}

func TestAutoProfiles(t *testing.T) {
	rules, err := ReadAutoProfiles(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 {
		t.Fatalf("%d rules", len(rules))
	}
	elite := `c:/games/elite dangerous/products/elite-dangerous-odyssey-64/ELITEDANGEROUS64.EXE`
	for _, c := range []struct {
		exe  string
		slot int
		want string
	}{
		{elite, 0, "Elite Mouse"},
		{elite, 1, ""}, // (none)
		{"EliteDangerous64.exe", 0, "Elite Mouse"},
		{`D:\Other\EliteDangerous64.exe`, 0, ""},
		{`C:\Tools\x.exe`, 3, "Tools"}, // one profile for every slot
		{elite, 9, ""},
	} {
		name, ok := AutoProfileFor(rules, c.exe, c.slot)
		if name != c.want || !ok {
			t.Errorf("%s slot %d: %q %v, want %q", c.exe, c.slot, name, ok, c.want)
		}
	}
	// as DS4Windows: the first matching rule for a DualSense, else the
	// first for any controller, and only that one
	other := AutoProfile{Path: "*elitedangerous64.exe", Profiles: [8]string{"Other", "Other"}}
	for _, c := range []struct {
		name  string
		rules []AutoProfile
		slot  int
		want  string
		ok    bool
	}{
		{"a later rule", append(slices.Clone(rules), other), 0, "Elite Mouse", true},
		{"a later rule, (none) keeps the profile", append(slices.Clone(rules), other), 1, "", true},
		{"a later rule for a DualSense", append(slices.Clone(rules), AutoProfile{Path: other.Path, Device: "DualSense", Profiles: other.Profiles}), 0, "Other", true},
		{"a rule for another controller", append([]AutoProfile{{Path: other.Path, Device: "DS4", Profiles: other.Profiles}}, rules...), 0, "Elite Mouse", true},
		{"a rule with a window title first", append([]AutoProfile{{Path: other.Path, Title: "Elite - Dangerous (CLIENT)", Profiles: other.Profiles}}, rules...), 0, "", false},
		{"a window title, the same profile", append([]AutoProfile{{Path: other.Path, Title: "x", Profiles: [8]string{"Elite Mouse"}}}, rules...), 0, "Elite Mouse", true},
		{"only a rule with a window title", []AutoProfile{{Path: other.Path, Title: "x", Profiles: other.Profiles}}, 0, "", false},
		{"a window title, (none)", []AutoProfile{{Path: other.Path, Title: "x", Profiles: [8]string{"(none)"}}}, 0, "", true},
	} {
		name, ok := AutoProfileFor(c.rules, elite, c.slot)
		if name != c.want || ok != c.ok {
			t.Errorf("%s: %q %v, want %q %v", c.name, name, ok, c.want, c.ok)
		}
	}
	for _, c := range []struct {
		path, exe string
		want      bool
	}{
		{`*\elite-dangerous-odyssey-64\`, elite, true},
		{`EliteDangerous64.exe$`, elite, true},
		{`^c:\games\`, elite, true},
		{`^d:\games\`, elite, false},
		{`^c:\games\`, "EliteDangerous64.exe", false},
	} {
		if got := (AutoProfile{Path: c.path}).matches(c.exe); got != c.want {
			t.Errorf("rule %q for %q: %v", c.path, c.exe, got)
		}
	}
}

// TestLinkedProfiles: "Link profile" keeps a profile for a controller by
// its MAC address, in LinkedProfiles.xml.
func TestLinkedProfiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "LinkedProfiles.xml"), `<?xml version="1.0" encoding="utf-8"?>
<LinkedControllers>
  <MACA1B2C3D4E5F6>Elite Mouse</MACA1B2C3D4E5F6>
  <MAC001122334455>Default</MAC001122334455>
  <Other>Ignored</Other>
</LinkedControllers>`)
	links, err := ReadLinkedProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	for mac, want := range map[string]string{
		"A1:B2:C3:D4:E5:F6": "Elite Mouse",
		"00:11:22:33:44:55": "Default",
		"a1:b2:c3:d4:e5:f6": "", // DS4Windows matches it exactly
		"00:00:00:00:00:00": "",
		"":                  "",
	} {
		if got := LinkedProfile(links, mac); got != want {
			t.Errorf("%q: %q, want %q", mac, got, want)
		}
	}
	if len(links) != 2 {
		t.Errorf("links %v", links)
	}
	if _, err := ReadLinkedProfiles(t.TempDir()); err == nil {
		t.Error("a folder without LinkedProfiles.xml read")
	}
}

// TestProfileGyro: EDSense's gyro may aim only where the profile leaves
// the gyro alone.
func TestProfileGyro(t *testing.T) {
	for _, c := range []struct {
		name       string
		gyro       Gyro
		dualSense  bool
		triggerLab bool
	}{
		{"Elite Passthru", GyroFree, true, false},
		{"Elite Mouse", GyroMouse, true, true},
		{"Elite Controls", GyroOther, true, false}, // Gyro X+ on the right stick while shifted
		{"Default", GyroFree, false, false},        // Controls, nothing on the gyro; Xbox 360 by default
	} {
		path, err := FindProfile(data, c.name)
		if err != nil {
			t.Fatal(err)
		}
		p, err := ReadProfile(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.Gyro() != c.gyro || EmulatesDualSense(p.Output) != c.dualSense || p.TriggerLab != c.triggerLab {
			t.Errorf("%s: gyro %s, output %q, Trigger Lab %v", c.name, p.Gyro(), p.Output, p.TriggerLab)
		}
	}
	for mode, want := range map[string]Gyro{"None": GyroFree, "MouseJoystick": GyroOther, "DirectionalSwipe": GyroOther, "Steering": GyroUnknown} {
		if got := (Profile{GyroOutput: mode}).Gyro(); got != want {
			t.Errorf("%s: %s", mode, got)
		}
	}
	for v, want := range map[string]bool{"Mouse Up": true, "MouseRight": true, "Left Y-Axis-": true, "RXPos": true, "Left Stick Down": true, "Left Stick": false, "A Button": false, "Unbound": false, "Left Mouse Button": false} {
		if stickOrMouse(v) != want {
			t.Errorf("%q on a stick or the mouse: %v", v, !want)
		}
	}
	if !EmulatesDualSense("ViiperDualSense") || !EmulatesDualSense("DualSense Edge") || EmulatesDualSense("ViiperX360") || EmulatesDualSense("") {
		t.Error("DualSense output")
	}
}

// TestUseSAforMouse: a profile an older DS4Windows saved, and nothing
// saved again since, keeps the gyro's mouse as UseSAforMouse. DS4Windows 5
// still reads it, over GyroOutputMode: true is Mouse, anything else
// Controls.
func TestUseSAforMouse(t *testing.T) {
	dir := t.TempDir()
	for i, c := range []struct {
		body string
		want Gyro
	}{
		{`<UseSAforMouse>True</UseSAforMouse>`, GyroMouse},
		{`<UseSAforMouse> true </UseSAforMouse><GyroOutputMode>None</GyroOutputMode>`, GyroMouse},
		{`<UseSAforMouse>False</UseSAforMouse><GyroOutputMode>Mouse</GyroOutputMode>`, GyroFree},
		{`<UseSAforMouse>yes</UseSAforMouse><GyroOutputMode>Mouse</GyroOutputMode>`, GyroFree},
		{`<UseSAforMouse></UseSAforMouse>`, GyroFree},
		{`<UseSAforMouse>False</UseSAforMouse><Control><Button><GyroXPos>Mouse Right</GyroXPos></Button></Control>`, GyroOther},
		{`<GyroOutputMode>Passthru</GyroOutputMode>`, GyroFree},
	} {
		path := filepath.Join(dir, fmt.Sprintf("p%d.xml", i))
		write(t, path, `<?xml version="1.0" encoding="utf-8"?>`+"\n"+`<DS4Windows app_version="1.4.52" config_version="2">`+c.body+`</DS4Windows>`)
		p, err := ReadProfile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Gyro(); got != c.want {
			t.Errorf("%s: the gyro is %s (output %q), want %s", c.body, got, p.GyroOutput, c.want)
		}
	}
}

// TestTriggerLab: Trigger Lab counts when it is on and sets a trigger, or
// turns game rumble into trigger vibration on a side (which sets that
// trigger Off while nothing rumbles).
func TestTriggerLab(t *testing.T) {
	dir := t.TempDir()
	for i, c := range []struct {
		body string
		want bool
	}{
		{`<Enabled>true</Enabled><RightActive>true</RightActive>`, true},
		{`<Enabled>true</Enabled><RightGameRumbleVibration>true</RightGameRumbleVibration>`, true},
		{`<Enabled>True</Enabled><LeftGameRumbleVibration> true </LeftGameRumbleVibration>`, true},
		{`<Enabled>false</Enabled><LeftGameRumbleVibration>true</LeftGameRumbleVibration><LeftActive>true</LeftActive>`, false},
		{`<Enabled>true</Enabled><LeftActive>false</LeftActive><RightGameRumbleVibration>false</RightGameRumbleVibration>`, false},
	} {
		path := filepath.Join(dir, fmt.Sprintf("p%d.xml", i))
		write(t, path, `<?xml version="1.0" encoding="utf-8"?>`+"\n"+`<DS4Windows><TriggerLab>`+c.body+`</TriggerLab></DS4Windows>`)
		p, err := ReadProfile(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.TriggerLab != c.want {
			t.Errorf("%s: Trigger Lab %v", c.body, p.TriggerLab)
		}
	}
}

// TestFindProfileASCII: DS4Windows answers in ASCII, with '?' for each
// other character, so "?stanbul" is the file named with a Turkish dotted
// I; a name that fits two files is not guessed.
func TestFindProfileASCII(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"\u0130stanbul", "S\u00fcper \u015eey", "\u00c7ay", "\u015eay", "Plain"} {
		write(t, filepath.Join(dir, "Profiles", name+".xml"), "<DS4Windows/>")
	}
	for _, c := range []struct {
		ask, want string
		ambiguous bool
	}{
		{"?stanbul", "\u0130stanbul.xml", false},
		{"S?per ?ey", "S\u00fcper \u015eey.xml", false},
		{"Plain", "Plain.xml", false},
		{"?ay", "", true},
		{"??stanbul", "", false},
		{"stanbul", "", false},
	} {
		path, err := FindProfile(dir, c.ask)
		switch {
		case c.want != "" && (err != nil || filepath.Base(path) != c.want):
			t.Errorf("%q: %q %v", c.ask, path, err)
		case c.want == "" && err == nil:
			t.Errorf("%q matched %q", c.ask, path)
		case c.ambiguous != errors.Is(err, errAmbiguous):
			t.Errorf("%q: %v", c.ask, err)
		}
	}
	if _, err := FindProfile(dir, `..\Plain`); err == nil {
		t.Error("a name with a path in it")
	}
}

// TestUTF16: .NET can save XML in UTF-16, and may say so in the
// declaration of a UTF-8 file too.
func TestUTF16(t *testing.T) {
	dir := t.TempDir()
	text := "<?xml version=\"1.0\" encoding=\"utf-16\"?>\r\n<DS4Windows><GyroOutputMode>None</GyroOutputMode><OutputContDevice>ViiperDualSense</OutputContDevice></DS4Windows>"
	u := utf16.Encode([]rune(text))
	b := []byte{0xFF, 0xFE}
	for _, v := range u {
		b = append(b, byte(v), byte(v>>8))
	}
	path := filepath.Join(dir, "a.xml")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if p, err := ReadProfile(path); err != nil || p.Gyro() != GyroFree || !EmulatesDualSense(p.Output) {
		t.Errorf("UTF-16: %+v %v", p, err)
	}
	write(t, path, "\ufeff"+text)
	if p, err := ReadProfile(path); err != nil || p.Gyro() != GyroFree {
		t.Errorf("UTF-8 saying UTF-16: %+v %v", p, err)
	}
}

// fakeEnv is DS4Windows as the tests script it.
type fakeEnv struct {
	dir     string
	answer  map[string]string // by question; missing: an error
	elite   string
	slot    int
	mac     string
	version string
	dsx     bool
	real    bool
}

func (f *fakeEnv) env() Env {
	return Env{
		DataDir: func() string { return f.dir },
		Query: func(slot int, prop string) (string, error) {
			if a, ok := f.answer[prop]; ok {
				return a, nil
			}
			return "", errors.New("DS4Windows is not running")
		},
		Elite:           func() string { return f.elite },
		Slot:            func() int { return f.slot },
		MAC:             func() string { return f.mac },
		Version:         func() string { return f.version },
		DSXOnPort:       func() bool { return f.dsx },
		PhysicalVisible: func() bool { return f.real },
	}
}

func dataCopy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"Profiles.xml", "Auto Profiles.xml", "Profiles/Elite Passthru.xml", "Profiles/Elite Mouse.xml", "Profiles/Elite Controls.xml", "Profiles/Default.xml"} {
		b, err := os.ReadFile(filepath.Join(data, name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, name), string(b))
	}
	return dir
}

// TestSetupCheck: the profile comes from DS4Windows itself, else from
// "Auto Profiles.xml" while Elite runs, else from Profiles.xml; each
// warning is told once.
func TestSetupCheck(t *testing.T) {
	f := &fakeEnv{dir: dataCopy(t), elite: "EliteDangerous64.exe", version: "4.9.1.0", dsx: true, real: true}
	var warned []Warning
	s := newSetup(f.env(), func(w Warning, detail string) { warned = append(warned, w) })
	if s.Gyro() != GyroUnknown {
		t.Fatal("known before the first check")
	}

	s.check(true) // Elite not running, DS4Windows cannot be asked: Profiles.xml
	if s.Gyro() != GyroFree || !strings.Contains(s.said, `"Elite Passthru" (from Profiles.xml`) {
		t.Errorf("from Profiles.xml: %s, %s", s.Gyro(), s.said)
	}
	s.Game(true, true, false)
	s.check(false) // Elite runs: Auto Profiles.xml
	if s.Gyro() != GyroMouse || !strings.Contains(s.said, `"Elite Mouse" (from Auto Profiles.xml`) {
		t.Errorf("from Auto Profiles.xml: %s, %s", s.Gyro(), s.said)
	}
	f.answer = map[string]string{PropProfile: "Elite Controls", PropOutput: "ViiperX360"}
	s.check(false) // DS4Windows answers
	if s.Gyro() != GyroOther || !strings.Contains(s.said, "DS4Windows says so") {
		t.Errorf("asked: %s, %s", s.Gyro(), s.said)
	}
	f.answer = map[string]string{PropProfile: "Gone"}
	s.check(false)
	if s.Gyro() != GyroUnknown {
		t.Errorf("a profile without a file: %s", s.Gyro())
	}
	s.check(true)
	want := []Warning{WarnOldVersion, WarnDSXOnPort, WarnPhysicalVisible, WarnTriggerLab, WarnTouchpadMouse, WarnNotDualSense}
	if len(warned) != len(want) {
		t.Fatalf("warnings %v, want %v", warned, want)
	}
	for i := range want {
		if warned[i] != want[i] {
			t.Fatalf("warnings %v, want %v", warned, want)
		}
	}
}

// TestSetupLinked: without DS4Windows to ask, the profile linked to the
// controller comes before Profiles.xml, and Elite's auto profile before
// both, as DS4Windows picks them.
func TestSetupLinked(t *testing.T) {
	f := &fakeEnv{dir: dataCopy(t), elite: "EliteDangerous64.exe", mac: "A1:B2:C3:D4:E5:F6"}
	write(t, filepath.Join(f.dir, "LinkedProfiles.xml"), "<LinkedControllers><MACA1B2C3D4E5F6>Elite Controls</MACA1B2C3D4E5F6></LinkedControllers>")
	s := newSetup(f.env(), nil)
	s.check(false)
	if s.Gyro() != GyroOther || !strings.Contains(s.said, `"Elite Controls" (linked in LinkedProfiles.xml`) {
		t.Errorf("linked: %s, %s", s.Gyro(), s.said)
	}
	s.Game(true, true, false)
	s.check(false)
	if s.Gyro() != GyroMouse || !strings.Contains(s.said, `"Elite Mouse" (from Auto Profiles.xml`) {
		t.Errorf("Elite runs: %s, %s", s.Gyro(), s.said)
	}
	s.Game(false, true, false)
	f.mac = "11:22:33:44:55:66"
	s.check(false)
	if s.Gyro() != GyroFree || !strings.Contains(s.said, `"Elite Passthru" (from Profiles.xml`) {
		t.Errorf("a controller without a link: %s, %s", s.Gyro(), s.said)
	}
	f.mac = ""
	s.check(false)
	if s.Gyro() != GyroUnknown {
		t.Errorf("links, and the MAC address not known: %s, %s", s.Gyro(), s.said)
	}
}

// TestSetupReadsChangedProfile: a profile file is parsed again only when
// it changes, and the change is seen.
func TestSetupReadsChangedProfile(t *testing.T) {
	f := &fakeEnv{dir: dataCopy(t), answer: map[string]string{PropProfile: "Elite Passthru", PropOutput: "ViiperDualSense"}}
	s := newSetup(f.env(), nil)
	s.check(false)
	if s.Gyro() != GyroFree {
		t.Fatalf("gyro %s", s.Gyro())
	}
	path := filepath.Join(f.dir, "Profiles", "Elite Passthru.xml")
	st, _ := os.Stat(path)
	c := s.cache[path]
	c.p.GyroOutput = "Mouse" // a parse would forget this
	s.cache[path] = c
	s.check(false)
	if s.Gyro() != GyroMouse {
		t.Fatal("an unchanged file was parsed again")
	}
	write(t, path, "<DS4Windows><GyroOutputMode>MouseJoystick</GyroOutputMode></DS4Windows>")
	later := st.ModTime().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	s.check(false)
	if s.Gyro() != GyroOther {
		t.Fatalf("the saved profile was not read: %s", s.Gyro())
	}
}

// TestSetupRequests: Step checks only while Elite runs with gyro aim on;
// Elite coming to the front checks at once.
func TestSetupRequests(t *testing.T) {
	s := newSetup(Env{}, nil)
	pending := func() (bool, bool) {
		select {
		case first := <-s.kick:
			return true, first
		default:
			return false, false
		}
	}
	s.Step()
	if got, _ := pending(); got {
		t.Fatal("checked while Elite is not running")
	}
	s.Game(true, true, false)
	s.Step()
	if got, first := pending(); !got || first {
		t.Fatal("no check while Elite runs with gyro aim on")
	}
	s.Game(true, true, true)
	if got, _ := pending(); !got {
		t.Fatal("Elite coming to the front asked for no check")
	}
	s.Game(true, true, true)
	if got, _ := pending(); got {
		t.Fatal("checked again while Elite stays in front")
	}
	s.Game(true, false, false)
	s.Game(true, false, true)
	s.Step()
	if got, _ := pending(); got {
		t.Fatal("checked with gyro aim off")
	}
}

// TestTouchpad: the output mode, an older profile's UseTPforControls, and
// DS4Windows' default Mouse when the file names neither.
func TestTouchpad(t *testing.T) {
	dir := t.TempDir()
	for i, c := range []struct {
		body  string
		want  string
		mouse bool
	}{
		{``, "Mouse", true},
		{`<TouchpadOutputMode>Passthru</TouchpadOutputMode>`, "Passthru", false},
		{`<TouchpadOutputMode> Controls </TouchpadOutputMode>`, "Controls", false},
		{`<TouchpadOutputMode>AbsoluteMouse</TouchpadOutputMode>`, "AbsoluteMouse", true},
		{`<TouchpadOutputMode>MouseJoystick</TouchpadOutputMode>`, "MouseJoystick", true},
		{`<UseTPforControls>True</UseTPforControls>`, "Controls", false},
		{`<UseTPforControls>False</UseTPforControls><TouchpadOutputMode>Passthru</TouchpadOutputMode>`, "Mouse", true},
	} {
		path := filepath.Join(dir, fmt.Sprintf("p%d.xml", i))
		write(t, path, `<?xml version="1.0" encoding="utf-8"?>`+"\n"+`<DS4Windows>`+c.body+`</DS4Windows>`)
		p, err := ReadProfile(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.Touchpad != c.want || p.TouchpadMouse() != c.mouse {
			t.Errorf("%q: touchpad %q (mouse %v), want %q (%v)", c.body, p.Touchpad, p.TouchpadMouse(), c.want, c.mouse)
		}
	}
}
