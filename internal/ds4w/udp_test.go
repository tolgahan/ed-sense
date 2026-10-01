package ds4w

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// DS4Windows' UDP server (Settings > UDP Server): its settings, where
// EDSense asks it, and the install step that turns it on.

func TestReadUDPSettings(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want Settings
	}{
		{"all four", `<UseUDPServer>True</UseUDPServer>
  <UDPServerPort>26800</UDPServerPort>
  <UDPServerListenAddress> 127.0.0.2 </UDPServerListenAddress>
  <UDPServerSmoothingOptions>
    <UseSmoothing>True</UseSmoothing>
    <UdpSmoothMinCutoff>0.4</UdpSmoothMinCutoff>
  </UDPServerSmoothingOptions>`,
			Settings{UDPServer: true, UDPPort: 26800, UDPAddress: "127.0.0.2", UDPSmoothing: true}},
		{"none", ``, Settings{}},
		{"junk", `<UseUDPServer>yes</UseUDPServer>
  <UDPServerPort>abc</UDPServerPort>
  <UDPServerSmoothingOptions><UseSmoothing>maybe</UseSmoothing></UDPServerSmoothingOptions>`, Settings{}},
		// DS4Windows holds the port to 1024..65535
		{"smoothing off, the first of each", `<UseUDPServer>false</UseUDPServer>
  <UseUDPServer>True</UseUDPServer>
  <UDPServerPort>0</UDPServerPort>
  <UDPServerSmoothingOptions><UseSmoothing>False</UseSmoothing></UDPServerSmoothingOptions>`, Settings{UDPPort: 1024}},
		{"port 800", `<UDPServerPort> 800 </UDPServerPort>`, Settings{UDPPort: 1024}},
		{"port 70000", `<UDPServerPort>70000</UDPServerPort>`, Settings{UDPPort: 65535}},
		{"port too large to read", `<UDPServerPort>99999999999</UDPServerPort>`, Settings{}},
	} {
		s, err := parseSettings([]byte("<Profile>\n  " + c.body + "\n</Profile>"))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got := Settings{UDPServer: s.UDPServer, UDPPort: s.UDPPort, UDPAddress: s.UDPAddress, UDPSmoothing: s.UDPSmoothing}
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	// the test folder has none of them
	if s, err := ReadSettings(data); err != nil || s.UDPServer || s.UDPPort != 0 || s.UDPAddress != "" || s.UDPSmoothing {
		t.Errorf("fixture: %+v %v", s, err)
	}
}

func TestUDPEndpoint(t *testing.T) {
	// not ok: an address EDSense does not use, where it still asks
	// 127.0.0.1 at the port
	for _, c := range []struct {
		address string
		port    int
		addr    string
		ok      bool
		shown   string
	}{
		{"", 0, "127.0.0.1:26760", true, ":26760"},
		{"127.0.0.1", 26760, "127.0.0.1:26760", true, "127.0.0.1:26760"},
		{"127.0.0.2", 1, "127.0.0.2:1024", true, "127.0.0.2:1024"},
		{"0.0.0.0", 65535, "127.0.0.1:65535", true, "0.0.0.0:65535"},
		{"localhost", 70000, "127.0.0.1:65535", true, "localhost:65535"},
		{"LOCALHOST", -5, "127.0.0.1:1024", true, "LOCALHOST:1024"},
		{"192.168.1.5", 26760, "127.0.0.1:26760", false, "192.168.1.5:26760"},
		{"::1", 26800, "127.0.0.1:26800", false, "[::1]:26800"},
		{"host.lan", 0, "127.0.0.1:26760", false, "host.lan:26760"},
	} {
		addr, ok, shown := Settings{UDPAddress: c.address, UDPPort: c.port}.UDPEndpoint()
		if addr == nil || addr.String() != c.addr || ok != c.ok || shown != c.shown {
			t.Errorf("%q port %d: %v %v %q, want %q %v %q", c.address, c.port, addr, ok, shown, c.addr, c.ok, c.shown)
		}
	}
	// a port that is not a number reads as 0, so the default
	s, _ := parseSettings([]byte("<Profile><UDPServerPort>junk</UDPServerPort></Profile>"))
	if addr, ok, _ := s.UDPEndpoint(); !ok || addr.String() != "127.0.0.1:26760" {
		t.Errorf("junk port: %v %v", addr, ok)
	}
}

// TestInspectUDP: where the UDP server listens once it is on, and the
// address an install would replace.
func TestInspectUDP(t *testing.T) {
	for _, c := range []struct {
		name, lines    string
		endpoint, move string
		step           string
	}{
		{"nothing set", "", "127.0.0.1:26760", "", StepTodo},
		{"on", udpOn, "127.0.0.1:26760", "", StepDone},
		{"own port and loopback address", udpOn + "  <UDPServerPort>26800</UDPServerPort>\n  <UDPServerListenAddress>127.0.0.2</UDPServerListenAddress>\n",
			"127.0.0.2:26800", "", StepDone},
		{"all addresses", udpOn + "  <UDPServerListenAddress>0.0.0.0</UDPServerListenAddress>\n", "127.0.0.1:26760", "", StepDone},
		{"another PC's address, port 0", udpOn + "  <UDPServerPort>0</UDPServerPort>\n  <UDPServerListenAddress>192.168.1.5</UDPServerListenAddress>\n",
			"127.0.0.1:1024", "192.168.1.5", StepTodo},
		// DS4Windows listens on 1024 then, where EDSense asks it
		{"port 800", udpOn + "  <UDPServerPort>800</UDPServerPort>\n", "127.0.0.1:1024", "", StepDone},
		{"a port DS4Windows cannot read", udpOn + "  <UDPServerPort>x</UDPServerPort>\n", "127.0.0.1:26760", "", StepTodo},
		{"a name", udpOn + "  <UDPServerListenAddress>host.lan</UDPServerListenAddress>\n", "127.0.0.1:26760", "host.lan", StepTodo},
		{"off", "  <UseUDPServer>False</UseUDPServer>\n", "127.0.0.1:26760", "", StepTodo},
		{"in another case", "  <UseUDPServer>true</UseUDPServer>\n", "127.0.0.1:26760", "", StepDone},
	} {
		dir := dsFolder(t, settingsText("Default", lineOn+c.lines), autoText())
		p := Inspect(Input{Dir: dir})
		if p.UDPEndpoint != c.endpoint || p.UDPMoves != c.move || p.Step(StepUDPServer) != c.step || len(p.Steps) != 4 {
			t.Errorf("%s: %q %q %s (%s)", c.name, p.UDPEndpoint, p.UDPMoves, p.Step(StepUDPServer), steps(p))
		}
	}
}

// TestSetUDPServer: the first <UseUDPServer> holds True, or one is
// added; a port DS4Windows would refuse becomes 26760, an address EDSense
// does not read it at 127.0.0.1; every other byte stays.
func TestSetUDPServer(t *testing.T) {
	head := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<Profile app_version=\"5.0.12.0\" config_version=\"5\">\n  <useExclusiveMode>False</useExclusiveMode>\n"
	const on = "<UseUDPServer>True</UseUDPServer>"
	for _, c := range []struct {
		name, in, want string
		cr             bool
	}{
		{name: "off", cr: true,
			in:   head + "  <UseUDPServer>False</UseUDPServer>\n  <UDPServerPort>26760</UDPServerPort>\n</Profile>\n",
			want: head + "  " + on + "\n  <UDPServerPort>26760</UDPServerPort>\n</Profile>\n"},
		{name: "empty element", cr: true,
			in:   head + "  <UseUDPServer/>\n</Profile>\n",
			want: head + "  " + on + "\n</Profile>\n"},
		{name: "missing, CRLF", cr: true,
			in:   head + "  <Controller1>Default</Controller1>\n</Profile>\n",
			want: head + "  <Controller1>Default</Controller1>\n  " + on + "\n</Profile>\n"},
		{name: "missing, LF",
			in:   head + "  <UseDSXUDPServer>True</UseDSXUDPServer>\n</Profile>\n",
			want: head + "  <UseDSXUDPServer>True</UseDSXUDPServer>\n  " + on + "\n</Profile>\n"},
		{name: "already on", cr: true,
			in:   head + "  " + on + "\n</Profile>\n",
			want: head + "  " + on + "\n</Profile>\n"},
		{name: "a port DS4Windows cannot read", cr: true,
			in:   head + "  <UseUDPServer>False</UseUDPServer>\n  <UDPServerPort>2676O</UDPServerPort>\n</Profile>\n",
			want: head + "  " + on + "\n  <UDPServerPort>26760</UDPServerPort>\n</Profile>\n"},
		// DS4Windows reads it as 1024, where other programs may ask it
		{name: "port 0 kept", cr: true,
			in:   head + "  <UseUDPServer>False</UseUDPServer>\n  <UDPServerPort>0</UDPServerPort>\n</Profile>\n",
			want: head + "  " + on + "\n  <UDPServerPort>0</UDPServerPort>\n</Profile>\n"},
		{name: "own port kept, all addresses kept", cr: true,
			in:   head + "  <UseUDPServer>False</UseUDPServer>\n  <UDPServerPort>26800</UDPServerPort>\n  <UDPServerListenAddress>0.0.0.0</UDPServerListenAddress>\n</Profile>\n",
			want: head + "  " + on + "\n  <UDPServerPort>26800</UDPServerPort>\n  <UDPServerListenAddress>0.0.0.0</UDPServerListenAddress>\n</Profile>\n"},
		{name: "another PC's address", cr: true,
			in:   head + "  <UseUDPServer>True</UseUDPServer>\n  <UDPServerListenAddress>192.168.1.5</UDPServerListenAddress>\n</Profile>\n",
			want: head + "  " + on + "\n  <UDPServerListenAddress>127.0.0.1</UDPServerListenAddress>\n</Profile>\n"},
		{name: "the other settings stay", cr: true,
			in: head + "  <UDPServerSmoothingOptions>\n    <UseSmoothing>True</UseSmoothing>\n  </UDPServerSmoothingOptions>\n" +
				"  <UseDSXUDPServer>False</UseDSXUDPServer>\n  <DSXUDPServerPort>0</DSXUDPServerPort>\n</Profile>\n",
			want: head + "  <UDPServerSmoothingOptions>\n    <UseSmoothing>True</UseSmoothing>\n  </UDPServerSmoothingOptions>\n" +
				"  <UseDSXUDPServer>False</UseDSXUDPServer>\n  <DSXUDPServerPort>0</DSXUDPServerPort>\n  " + on + "\n</Profile>\n"},
	} {
		in, want := []byte(c.in), []byte(c.want)
		if c.cr {
			in, want = []byte(crlf(c.in)), []byte(crlf(c.want))
		}
		got, err := SetUDPServer(in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n%q\nwant\n%q", c.name, got, want)
			continue
		}
		if err := readsBack(got, checkUDPServer); err != nil {
			t.Errorf("%s: the result: %v", c.name, err)
		}
		if s, err := parseSettings(got); err != nil || !s.UDPServer {
			t.Errorf("%s: read back %+v %v", c.name, s, err)
		}
	}
	if _, err := SetUDPServer([]byte("<Programs/>")); err == nil {
		t.Error("another root edited")
	}
}

func TestCheckUDPServer(t *testing.T) {
	for _, c := range []struct {
		name, body string
		ok         bool
	}{
		{"on", "<UseUDPServer>True</UseUDPServer>", true},
		{"on, port and address", "<UseUDPServer>TRUE</UseUDPServer><UDPServerPort>26760</UDPServerPort><UDPServerListenAddress>localhost</UDPServerListenAddress>", true},
		{"off", "<UseUDPServer>False</UseUDPServer>", false},
		{"missing", "", false},
		{"not a bool", "<UseUDPServer>yes</UseUDPServer>", false},
		{"port 0, read as 1024", "<UseUDPServer>True</UseUDPServer><UDPServerPort>0</UDPServerPort>", true},
		{"port -5 with spaces, read as 1024", "<UseUDPServer>True</UseUDPServer><UDPServerPort> -5 </UDPServerPort>", true},
		{"port not a number", "<UseUDPServer>True</UseUDPServer><UDPServerPort>x</UDPServerPort>", false},
		{"port past 32 bits", "<UseUDPServer>True</UseUDPServer><UDPServerPort>4294967296</UDPServerPort>", false},
		{"another PC's address", "<UseUDPServer>True</UseUDPServer><UDPServerListenAddress>10.0.0.1</UDPServerListenAddress>", false},
		{"elements inside", "<UseUDPServer><X/>True</UseUDPServer>", false},
	} {
		err := readsBack([]byte("<Profile>"+c.body+"</Profile>"), checkUDPServer)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if err := readsBack([]byte("<Programs><UseUDPServer>True</UseUDPServer></Programs>"), checkUDPServer); err == nil {
		t.Error("another root passed")
	}
	for _, s := range []string{"", " 0.0.0.0 ", "localhost", "LocalHost", "127.0.0.1", "127.1.2.3"} {
		if !usableAddress(s) {
			t.Errorf("%q not usable", s)
		}
	}
	for _, s := range []string{"::1", "192.168.1.5", "host.lan", "::ffff:127.0.0.1", "127.0.0.1:26760"} {
		if usableAddress(s) {
			t.Errorf("%q usable", s)
		}
	}
}

// TestInstallUDPServer: game mod support and the UDP server write
// Profiles.xml once, read back for both; the UDP server alone, after an
// install made before its step.
func TestInstallUDPServer(t *testing.T) {
	settings := settingsText("Default", lineOff)
	auto := autoText(prog("EliteDangerous64.exe", "", "", "Mine"))
	dir := dsFolder(t, settings, auto, "Default")
	w := newWriter(t, nil)
	puts := map[string]int{}
	w.put = func(path string, b []byte) error {
		puts[filepath.Base(path)]++
		return put(path, b)
	}
	res, err := w.install(Input{Dir: dir}, false, nil)
	if err != nil || !slices.Equal(res.Wrote, []string{StepListener, StepUDPServer}) {
		t.Fatalf("both: %+v %v", res, err)
	}
	if len(puts) != 1 || puts[settingsFile] != 1 {
		t.Errorf("puts %v", puts)
	}
	want, _ := SetListener([]byte(settings))
	want, _ = SetUDPServer(want)
	got := snapshot(t, dir)
	if got[settingsFile] != string(want) || got[autoProfilesFile] != auto {
		t.Errorf("Profiles.xml:\n%s", got[settingsFile])
	}
	if back := snapshot(t, res.Backup); len(back) != 2 || back[settingsFile] != settings {
		t.Errorf("copies %v", keys(back))
	}
	if p := Inspect(Input{Dir: dir}); steps(p) != "skip skip done done" || p.CanInstall() {
		t.Errorf("after: %s %s", p.State, steps(p))
	}

	// one change for both, whose check needs both on
	p, f := inspect(Input{Dir: dsFolder(t, settings, autoText())})
	changes, err := makeChanges(p.Dir, f, []string{StepListener, StepUDPServer})
	if err != nil || len(changes) != 1 || !slices.Equal(changes[0].steps, []string{StepListener, StepUDPServer}) {
		t.Fatalf("changes %+v %v", changes, err)
	}
	listenerOnly, _ := SetListener([]byte(settings))
	udpOnly, _ := SetUDPServer([]byte(settings))
	if readsBack(listenerOnly, changes[0].check) == nil || readsBack(udpOnly, changes[0].check) == nil || readsBack(want, changes[0].check) != nil {
		t.Error("the read-back does not check both")
	}

	// a v0.7.0 install: only the UDP server is left
	v070 := settingsText("Default", lineOn)
	dir = dsFolder(t, v070, autoText(ruleBody))
	write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), string(ProfileFile("5.0.12.0")))
	before := Inspect(Input{Dir: dir})
	if before.State != PlanPartial || !slices.Equal(before.Todo(false), []string{StepUDPServer}) || !slices.Equal(before.Files(false), []string{settingsFile}) {
		t.Fatalf("v0.7.0: %s %v %v", before.State, before.Todo(false), before.Files(false))
	}
	res, err = newWriter(t, nil).install(Input{Dir: dir}, false, []string{StepUDPServer})
	if err != nil || !slices.Equal(res.Wrote, []string{StepUDPServer}) {
		t.Fatalf("udp_server only: %+v %v", res, err)
	}
	want, _ = SetUDPServer([]byte(v070))
	if got := snapshot(t, dir); got[settingsFile] != string(want) {
		t.Errorf("udp_server only:\n%s", got[settingsFile])
	}
	if p := Inspect(Input{Dir: dir}); p.State != PlanOurs {
		t.Errorf("after: %s %s", p.State, steps(p))
	}
	if !strings.Contains(res.Did(), "turned DS4Windows' UDP server on, in ") {
		t.Errorf("did: %s", res.Did())
	}

	// a cut write of Profiles.xml is put back, and neither step is written
	dir = dsFolder(t, v070, autoText(ruleBody))
	write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), string(ProfileFile("5.0.12.0")))
	w = newWriter(t, nil)
	w.put = func(path string, b []byte) error { return put(path, b[:len(b)/2]) }
	res, err = w.install(Input{Dir: dir}, false, nil)
	if err == nil || !res.Restored || len(res.Wrote) != 0 || snapshot(t, dir)[settingsFile] != v070 {
		t.Errorf("cut: %+v %v", res, err)
	}

	// DS4Windows starts before Profiles.xml, the last file: neither of its
	// steps is written, and the stop names the files before it
	dir = dsFolder(t, settings, autoText())
	calls := 0
	w = newWriter(t, func() (bool, string) { calls++; return calls < 4, "it runs" })
	res, err = w.install(Input{Dir: dir}, false, nil)
	var stop *InterruptedError
	if !errors.As(err, &stop) || !slices.Equal(stop.Wrote, []string{StepProfile, StepRule}) || snapshot(t, dir)[settingsFile] != settings {
		t.Errorf("interrupted: %+v %v", res, err)
	}
}

// TestInstallMovesShown: DS4Windows writes its UDP server's address when
// it exits, after the question. An install that would now replace an
// address the question did not name writes nothing; the one it named is
// replaced.
func TestInstallMovesShown(t *testing.T) {
	moved := settingsText("Default", lineOn+udpOn+"  <UDPServerListenAddress>192.168.1.5</UDPServerListenAddress>\n")
	dir := dsFolder(t, moved, autoText(ruleBody))
	write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), string(ProfileFile("5.0.12.0")))
	for _, shown := range []string{"", "10.0.0.2"} {
		w := newWriter(t, nil)
		w.moves = &shown
		_, err := w.install(Input{Dir: dir}, false, []string{StepUDPServer})
		var ce *ChangedError
		if !errors.As(err, &ce) || ce.Moves != "192.168.1.5" || !strings.Contains(err.Error(), "listens on 192.168.1.5 now") {
			t.Errorf("shown %q: %v", shown, err)
		}
		if got := snapshot(t, dir)[settingsFile]; got != moved {
			t.Errorf("shown %q: written", shown)
		}
	}
	w := newWriter(t, nil)
	named := "192.168.1.5"
	w.moves = &named
	if res, err := w.install(Input{Dir: dir}, false, []string{StepUDPServer}); err != nil || !slices.Equal(res.Wrote, []string{StepUDPServer}) {
		t.Fatalf("named: %+v %v", res, err)
	}
	if s, err := ReadSettings(dir); err != nil || s.UDPAddress != "127.0.0.1" {
		t.Errorf("named: %+v %v", s, err)
	}

	// the installer keeps what the request saw
	i, _, _, _ := newInstaller(t)
	dir = dsFolder(t, moved, autoText(ruleBody))
	write(t, filepath.Join(dir, profilesDir, ProfileName+".xml"), string(ProfileFile("5.0.12.0")))
	if err := i.Request(Input{Dir: dir}, false); err != nil || i.Job().Moves != "192.168.1.5" {
		t.Errorf("request: %+v %v", i.Job(), err)
	}
}

// TestInstallerUDPSteps: the question shows four steps; one shown before
// the UDP server step is refused.
func TestInstallerUDPSteps(t *testing.T) {
	i, _, _, _ := newInstaller(t)
	dir := dsFolder(t, settingsText("Default", lineOff), autoText())
	p := Inspect(Input{Dir: dir})
	if !slices.Equal(p.Todo(false), []string{StepProfile, StepRule, StepListener, StepUDPServer}) ||
		!slices.Equal(p.Files(false), []string{profilesDir + `\` + ProfileName + ".xml", autoProfilesFile, settingsFile}) {
		t.Fatalf("todo %v, files %v", p.Todo(false), p.Files(false))
	}
	if err := i.RequestSteps(Input{Dir: dir}, false, []string{StepProfile, StepRule, StepListener}); !errors.Is(err, ErrChanged) {
		t.Errorf("three steps shown: %v", err)
	}
	if err := i.RequestSteps(Input{Dir: dir}, false, p.Todo(false)); err != nil || len(i.Job().Steps) != 4 {
		t.Errorf("four steps shown: %+v %v", i.Job(), err)
	}
}
