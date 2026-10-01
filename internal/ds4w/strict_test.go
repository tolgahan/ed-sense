package ds4w

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf16"
)

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

var bom = []byte{0xEF, 0xBB, 0xBF}

func utf16LE(s string) []byte {
	b := []byte{0xFF, 0xFE}
	for _, v := range utf16.Encode([]rune(s)) {
		b = append(b, byte(v), byte(v>>8))
	}
	return b
}

// TestScan: the strict reader refuses what .NET refuses, and what EDSense
// does not edit.
func TestScan(t *testing.T) {
	for _, c := range []struct {
		name string
		b    []byte
		ok   bool
	}{
		{"plain", []byte("<a><b>1</b></a>"), true},
		{"declaration and comments", []byte("<?xml version=\"1.0\" encoding=\"utf-8\"?>\r\n<!-- x -->\r\n<a/>\r\n<!-- </a> -->\r\n"), true},
		{"byte order mark", append(bom, []byte("<?xml version=\"1.0\"?><a/>")...), true},
		{"declared UTF-16 in UTF-8 bytes", []byte("<?xml version=\"1.0\" encoding=\"utf-16\"?><a/>"), true},
		{"UTF-16 LE", utf16LE("<a/>"), false},
		{"UTF-16 without a mark", []byte{'<', 0, 'a', 0, '/', 0, '>', 0}, false},
		{"two roots", []byte("<a/><b/>"), false},
		{"text outside the root", []byte("x<a/>"), false},
		{"declaration not at the start", []byte(" <?xml version=\"1.0\"?><a/>"), false},
		{"doctype", []byte("<!DOCTYPE a><a/>"), false},
		{"attribute twice", []byte("<a x=\"1\" x=\"2\"/>"), false},
		{"attribute twice inside", []byte("<a><b x=\"1\" x=\"1\"/></a>"), false},
		{"unclosed", []byte("<a><b></b>"), false},
		{"mismatched", []byte("<a></b>"), false},
		{"empty", nil, false},
		{"bad UTF-8", []byte("<a>\xff</a>"), false},
		{"unknown entity", []byte("<a>&x;</a>"), false},
	} {
		_, err := scan(c.b)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := scan(utf16LE("<a/>")); err != errUTF16 {
		t.Errorf("UTF-16: %v", err)
	}
	d, err := scan([]byte("<a>\n  <b x=\"1\">t<![CDATA[<]]></b>\n  <c/>\n</a>"))
	if err != nil {
		t.Fatal(err)
	}
	b, c := d.root.first("b"), d.root.first("c")
	if b == nil || b.text != "t<" || b.empty || c == nil || !c.empty || d.root.first("d") != nil {
		t.Fatalf("b %+v c %+v", b, c)
	}
	if v, ok := b.attrValue("x"); v != "1" || !ok {
		t.Errorf("attribute %q %v", v, ok)
	}
	src := string(d.b)
	if src[b.start:b.open] != `<b x="1">` || src[b.close:b.end] != "</b>" || src[c.start:c.end] != "<c/>" || src[d.root.close:d.root.end] != "</a>" {
		t.Errorf("offsets: %q %q %q", src[b.start:b.open], src[b.close:b.end], src[c.start:c.end])
	}
	if d.eol() != "\n" {
		t.Error("LF file")
	}
	d, _ = scan(append(bom, []byte("<a>\r\n<b/></a>")...))
	if d.bom != 3 || string(d.b[d.root.first("b").start:d.root.first("b").end]) != "<b/>" || d.eol() != "\r\n" {
		t.Errorf("offsets after a byte order mark")
	}
}

func autoDoc(t *testing.T, s string) error {
	t.Helper()
	d, err := scan([]byte(s))
	if err != nil {
		return err
	}
	return validAutoProfiles(d)
}

// TestValidAutoProfiles: device and applyToAllControllers are read as
// .NET reads them, in their exact case (M6).
func TestValidAutoProfiles(t *testing.T) {
	rule := func(attrs string) string {
		return "<Programs>\n  <Program path=\"x.exe\" title=\"\"" + attrs + ">\n    <Controller1>P</Controller1>\n    <TurnOff>False</TurnOff>\n  </Program>\n</Programs>"
	}
	for _, c := range []struct {
		attrs string
		ok    bool
	}{
		{"", true},
		{` device="DualSense"`, true},
		{` device="Any"`, true},
		{` device="JoyCons"`, true},
		{` device="dualsense"`, false},
		{` device="DualSense "`, false},
		{` device="Xbox"`, false},
		{` applyToAllControllers="true"`, true},
		{` applyToAllControllers="false"`, true},
		{` applyToAllControllers="1"`, true},
		{` applyToAllControllers="0"`, true},
		{` applyToAllControllers=" true "`, true},
		{` applyToAllControllers="True"`, false},
		{` applyToAllControllers="TRUE"`, false},
		{` applyToAllControllers="yes"`, false},
		{` applyToAllControllers=""`, false},
	} {
		if err := autoDoc(t, rule(c.attrs)); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.attrs, err)
		}
	}
	for _, c := range []struct {
		name, s string
		ok      bool
	}{
		{"empty", "<Programs />", true},
		{"no rules", "<Programs></Programs>", true},
		{"unknown elements", "<Programs><Other device=\"x\"/></Programs>", true},
		{"other root", "<Profiles/>", false},
		{"root in a namespace", "<Programs xmlns=\"urn:x\"/>", false},
		{"xsi and xsd", "<Programs xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\" xmlns:xsd=\"http://www.w3.org/2001/XMLSchema\"/>", true},
		{"elements in a profile entry", "<Programs><Program path=\"x\"><Controller2><b/></Controller2></Program></Programs>", false},
		{"elements in TurnOff", "<Programs><Program path=\"x\"><TurnOff><b/></TurnOff></Program></Programs>", false},
		{"second rule bad", "<Programs><Program path=\"x\"/><Program path=\"y\" device=\"ds4\"/></Programs>", false},
	} {
		if err := autoDoc(t, c.s); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	err := autoDoc(t, "<Programs><Program path=\"a\"/><Program path=\"b\" applyToAllControllers=\"True\"/></Programs>")
	if err == nil || !strings.Contains(err.Error(), `rule 2 has applyToAllControllers="True"`) {
		t.Errorf("message: %v", err)
	}
}

// TestValidSettings and the profile: roots, the listener's elements, and
// the exact enum names of a profile.
func TestValidSettingsAndProfile(t *testing.T) {
	settings := func(s string) error {
		d, err := scan([]byte(s))
		if err == nil {
			err = validSettings(d)
		}
		return err
	}
	for _, c := range []struct {
		s  string
		ok bool
	}{
		{"<Profile/>", true},
		{"<Profile><UseDSXUDPServer>True</UseDSXUDPServer><DSXUDPServerPort>6969</DSXUDPServerPort></Profile>", true},
		{"<Profile><UseDSXUDPServer> false </UseDSXUDPServer></Profile>", true},
		{"<Profile><UseDSXUDPServer>yes</UseDSXUDPServer></Profile>", false},
		{"<Profile><DSXUDPServerPort>69a</DSXUDPServerPort></Profile>", false},
		{"<Profile><DSXUDPServerPort>-1</DSXUDPServerPort></Profile>", false},
		{"<Profile><DSXUDPServerPort>70000</DSXUDPServerPort></Profile>", false},
		{"<Profile><UseDSXUDPServer><b/></UseDSXUDPServer></Profile>", false},
		{"<Profiles/>", false},
	} {
		if err := settings(c.s); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.s, err)
		}
	}

	profile := func(s string) error {
		d, err := scan([]byte(s))
		if err == nil {
			err = validProfile(d)
		}
		return err
	}
	for _, c := range []struct {
		s  string
		ok bool
	}{
		{"<DS4Windows/>", true},
		{"<DS4Windows><GyroOutputMode>MouseJoystick</GyroOutputMode><TouchpadOutputMode>AbsoluteMouse</TouchpadOutputMode></DS4Windows>", true},
		{"<DS4Windows><GyroOutputMode>mousejoystick</GyroOutputMode></DS4Windows>", false},
		{"<DS4Windows><TouchpadOutputMode>absolutemouse</TouchpadOutputMode></DS4Windows>", false},
		{"<DS4Windows><TouchpadOutputMode>Passthru </TouchpadOutputMode></DS4Windows>", false},
		{"<DS4Windows><Color>1,2,3</Color></DS4Windows>", true},
		{"<DS4Windows><Color>1, 2, 3</Color></DS4Windows>", false},
		{"<Profile/>", false},
	} {
		if err := profile(c.s); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.s, err)
		}
	}

	ours := func(b []byte) error {
		d, err := scan(b)
		if err == nil {
			err = validOurProfile(d)
		}
		return err
	}
	good := string(ProfileFile("5.0.12.0"))
	if err := ours([]byte(good)); err != nil {
		t.Fatalf("EDSense's profile: %v", err)
	}
	for _, c := range []struct{ from, to string }{
		{`config_version="5"`, `config_version="4"`},
		{`<GyroOutputMode>Passthru`, `<GyroOutputMode>passthru`},
		{`<GyroOutputMode>Passthru`, `<GyroOutputMode> Passthru`},
		{`<TouchpadOutputMode>Passthru`, `<TouchpadOutputMode>Mouse`},
		{`<OutputContDevice>ViiperDualSense`, `<OutputContDevice>ViiperX360`},
		{`<Color>0,0,255`, `<Color>0,0,256`},
		{`<Color>0,0,255`, `<Color>0,255`},
		{`<DS4Windows `, `<DS4WindowsX `},
	} {
		bad := strings.Replace(good, c.from, c.to, 1)
		if bad == good {
			t.Fatalf("%s not in the profile", c.from)
		}
		if err := ours([]byte(bad)); err == nil {
			t.Errorf("%s -> %s: taken", c.from, c.to)
		}
	}
}

// TestProfileFile: EDSense's profile is UTF-8 without a byte order mark,
// CRLF, and reads as DualSense emulation with the gyro and touchpad
// passed through.
func TestProfileFile(t *testing.T) {
	want := crlf(`<?xml version="1.0" encoding="utf-8"?>
<!-- Written by EDSense for Elite Dangerous. Every setting not listed here is DS4Windows' default. -->
<DS4Windows app_version="5.0.12.0" config_version="5">
  <Color>0,0,255</Color>
  <OutputContDevice>ViiperDualSense</OutputContDevice>
  <GyroOutputMode>Passthru</GyroOutputMode>
  <TouchpadOutputMode>Passthru</TouchpadOutputMode>
</DS4Windows>
`)
	if got := string(ProfileFile("5.0.12.0")); got != want {
		t.Fatalf("profile:\n%s", got)
	}
	for _, v := range []string{"", "5.0.12.0\"><x", "abc", "1.2.3.4.5", "5..0"} {
		if !strings.Contains(string(ProfileFile(v)), `app_version="5.0.0.0"`) {
			t.Errorf("version %q not replaced", v)
		}
	}
	dir := t.TempDir()
	path := dir + "/p.xml"
	write(t, path, string(ProfileFile("5.0.12.0")))
	p, err := ReadProfile(path)
	if err != nil || !EmulatesDualSense(p.Output) || p.Gyro() != GyroFree || p.TouchpadMouse() || p.TriggerLab {
		t.Errorf("reads as %+v %v", p, err)
	}
}

// ruleBody is EDSense's rule as it must be written.
const ruleBody = `  <Program path="EliteDangerous64.exe$" title="" device="DualSense" applyToAllControllers="true">
    <Controller1>Elite Dangerous (EDSense)</Controller1>
    <Controller2>(none)</Controller2>
    <Controller3>(none)</Controller3>
    <Controller4>(none)</Controller4>
    <Controller5>(none)</Controller5>
    <Controller6>(none)</Controller6>
    <Controller7>(none)</Controller7>
    <Controller8>(none)</Controller8>
    <TurnOff>False</TurnOff>
  </Program>
`

const playerRules = `  <Program path="C:\Tools\tool.exe" title="">
    <Controller1>Tools</Controller1>
    <Controller2>(none)</Controller2>
    <TurnOff>False</TurnOff>
  </Program>
`

// TestSpliceRule: the rule goes in as the root's last child, found by the
// XML reader, and every other byte stays: line ends, the byte order
// mark, comments that hold "</Programs>" (L11).
func TestSpliceRule(t *testing.T) {
	head := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<!-- Auto-Profile Configuration Data. 01/10/2026 12:00:00 -->\n\n"
	for _, c := range []struct {
		name     string
		in, want string // in has @ where the rule goes; want, when set, is the whole result
		bom, cr  bool
	}{
		{name: "CRLF", in: head + "<Programs>\n" + playerRules + "@</Programs>\n", cr: true},
		{name: "LF", in: head + "<Programs>\n" + playerRules + "@</Programs>\n"},
		{name: "byte order mark", in: head + "<Programs>\n" + playerRules + "@</Programs>\n", bom: true, cr: true},
		{name: "no rules", in: head + "<Programs>\n@</Programs>", cr: true},
		{name: "indented end tag", in: head + "<Programs>\n" + playerRules + "@  </Programs>\n"},
		{name: "comment after the root", in: head + "<Programs>\n" + playerRules + "@</Programs>\n<!-- </Programs> -->\n", cr: true},
		{name: "comment in the root", in: head + "<Programs>\n" + playerRules + "  <!-- </Programs> -->\n@</Programs>\n", cr: true},
		{name: "no line end at the end", in: "<Programs>\n" + playerRules + "@</Programs>"},
		{name: "empty", in: head + "<Programs />\n", cr: true,
			want: head + "<Programs>\n" + ruleBody + "</Programs>\n"},
		{name: "empty, no space", in: "<Programs/>",
			want: "<Programs>\r\n" + crlf(ruleBody) + "</Programs>"},
		{name: "empty with attributes", in: "<Programs xmlns:xsd=\"http://www.w3.org/2001/XMLSchema\"  />\n",
			want: "<Programs xmlns:xsd=\"http://www.w3.org/2001/XMLSchema\">\n" + ruleBody + "</Programs>\n"},
		{name: "on one line", in: "<Programs></Programs>",
			want: "<Programs>\r\n" + crlf(ruleBody) + "</Programs>"},
		{name: "end tag after a rule on its line", in: "<Programs>\n  <Program path=\"x\"/></Programs>\n",
			want: "<Programs>\n  <Program path=\"x\"/>\n" + ruleBody + "</Programs>\n"},
	} {
		in, want := c.in, c.want
		if want == "" {
			want = strings.Replace(in, "@", ruleBody, 1)
			in = strings.Replace(in, "@", "", 1)
		}
		if c.cr {
			in, want = crlf(in), crlf(want)
		}
		inB, wantB := []byte(in), []byte(want)
		if c.bom {
			inB, wantB = append(bom[:3:3], inB...), append(bom[:3:3], wantB...)
		}
		got, err := SpliceRule(inB)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !bytes.Equal(got, wantB) {
			t.Errorf("%s:\n%q\nwant\n%q", c.name, got, wantB)
			continue
		}
		d, err := scan(got)
		if err == nil {
			err = hasOurRule(d, strings.Count(in, "<Program ")+1)
		}
		if err != nil {
			t.Errorf("%s: the result: %v", c.name, err)
		}
		rules, err := parseAutoProfiles(got)
		if err != nil || len(rules) == 0 || !isOurs(rules[len(rules)-1]) {
			t.Errorf("%s: read back %+v %v", c.name, rules, err)
		}
	}
	for _, bad := range []string{
		"<Programs><Program path=\"x\" applyToAllControllers=\"True\"/></Programs>",
		"<Programs/><Programs/>",
		"<Profile/>",
	} {
		if _, err := SpliceRule([]byte(bad)); err == nil {
			t.Errorf("%s: spliced", bad)
		}
	}
	if _, err := SpliceRule(utf16LE("<Programs/>")); err == nil {
		t.Error("UTF-16 spliced")
	}
}

// TestSetListener: the first <UseDSXUDPServer> holds True, or one is
// added; never a second one; a port or address DS4Windows would refuse
// becomes its default; every other byte stays.
func TestSetListener(t *testing.T) {
	head := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<Profile app_version=\"5.0.12.0\" config_version=\"5\">\n  <useExclusiveMode>False</useExclusiveMode>\n"
	const on = "<UseDSXUDPServer>True</UseDSXUDPServer>"
	for _, c := range []struct {
		name, in, want string
		cr, bom        bool
	}{
		{name: "off", cr: true,
			in:   head + "  <UseDSXUDPServer>False</UseDSXUDPServer>\n  <DSXUDPServerPort>6969</DSXUDPServerPort>\n</Profile>\n",
			want: head + "  " + on + "\n  <DSXUDPServerPort>6969</DSXUDPServerPort>\n</Profile>\n"},
		{name: "off, LF, byte order mark", bom: true,
			in:   head + "  <UseDSXUDPServer>false</UseDSXUDPServer>\n</Profile>\n",
			want: head + "  " + on + "\n</Profile>\n"},
		{name: "missing", cr: true,
			in:   head + "  <Controller1>Default</Controller1>\n</Profile>\n",
			want: head + "  <Controller1>Default</Controller1>\n  " + on + "\n</Profile>\n"},
		{name: "missing, LF",
			in:   head + "</Profile>",
			want: head + "  " + on + "\n</Profile>"},
		{name: "missing, on one line",
			in:   "<Profile><Controller1>Default</Controller1></Profile>",
			want: "<Profile><Controller1>Default</Controller1>\r\n  " + on + "\r\n</Profile>"},
		{name: "missing, empty root",
			in:   "<Profile />",
			want: "<Profile>\r\n  " + on + "\r\n</Profile>"},
		{name: "twice: the first counts", cr: true,
			in:   head + "  <UseDSXUDPServer>False</UseDSXUDPServer>\n  <UseDSXUDPServer>False</UseDSXUDPServer>\n</Profile>\n",
			want: head + "  " + on + "\n  <UseDSXUDPServer>False</UseDSXUDPServer>\n</Profile>\n"},
		{name: "empty element", cr: true,
			in:   head + "  <UseDSXUDPServer />\n</Profile>\n",
			want: head + "  " + on + "\n</Profile>\n"},
		{name: "already on", cr: true,
			in:   head + "  " + on + "\n</Profile>\n",
			want: head + "  " + on + "\n</Profile>\n"},
		{name: "port and address refused", cr: true,
			in:   head + "  <UseDSXUDPServer>False</UseDSXUDPServer>\n  <DSXUDPServerPort>0</DSXUDPServerPort>\n  <DSXUDPServerListenAddress>192.168.1.5</DSXUDPServerListenAddress>\n</Profile>\n",
			want: head + "  " + on + "\n  <DSXUDPServerPort>6969</DSXUDPServerPort>\n  <DSXUDPServerListenAddress>127.0.0.1</DSXUDPServerListenAddress>\n</Profile>\n"},
		{name: "port not a number, address kept", cr: true,
			in:   head + "  <DSXUDPServerPort>abc</DSXUDPServerPort>\n  <DSXUDPServerListenAddress>::1</DSXUDPServerListenAddress>\n</Profile>\n",
			want: head + "  <DSXUDPServerPort>6969</DSXUDPServerPort>\n  <DSXUDPServerListenAddress>::1</DSXUDPServerListenAddress>\n  " + on + "\n</Profile>\n"},
		{name: "own port kept, empty address", cr: true,
			in:   head + "  <DSXUDPServerPort>7000</DSXUDPServerPort>\n  <DSXUDPServerListenAddress />\n</Profile>\n",
			want: head + "  <DSXUDPServerPort>7000</DSXUDPServerPort>\n  <DSXUDPServerListenAddress>127.0.0.1</DSXUDPServerListenAddress>\n  " + on + "\n</Profile>\n"},
		{name: "comment after the root",
			in:   "<Profile>\n</Profile>\n<!-- </Profile> -->\n",
			want: "<Profile>\n  " + on + "\n</Profile>\n<!-- </Profile> -->\n"},
		{name: "deeper ones are not the setting",
			in:   "<Profile>\n  <X><UseDSXUDPServer>False</UseDSXUDPServer></X>\n</Profile>\n",
			want: "<Profile>\n  <X><UseDSXUDPServer>False</UseDSXUDPServer></X>\n  " + on + "\n</Profile>\n"},
	} {
		in, want := []byte(c.in), []byte(c.want)
		if c.cr {
			in, want = []byte(crlf(c.in)), []byte(crlf(c.want))
		}
		if c.bom {
			in, want = append(bom[:3:3], in...), append(bom[:3:3], want...)
		}
		got, err := SetListener(in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n%q\nwant\n%q", c.name, got, want)
			continue
		}
		d, err := scan(got)
		if err == nil {
			err = checkListener(d)
		}
		if err != nil {
			t.Errorf("%s: the result: %v", c.name, err)
		}
		if s, err := parseSettings(got); err != nil || !s.Listener {
			t.Errorf("%s: read back %+v %v", c.name, s, err)
		}
	}
	if _, err := SetListener([]byte("<Programs/>")); err == nil {
		t.Error("another root edited")
	}
}
