package dsx

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/assets"
	"github.com/tolgahan/ed-sense/internal/elite"
)

func weaponsFrame() Frame {
	f := DarkFrame()
	f.Left = NewTrigger("WEAPON", []int{2, 5, 4})
	f.Right = NewTrigger("weapon", []int{2, 5, 6})
	f.PlayerLEDs = LitLEDs(2)
	return f
}

func TestPacketsMatchDSXExample(t *testing.T) {
	dsx, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer dsx.Close()
	c, err := NewClient(dsx.LocalAddr().(*net.UDPAddr).Port, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	all := Outputs{Triggers: true, Lightbar: true, PlayerLEDs: true, Mic: true}
	c.Send([]int{0}, nil, weaponsFrame(), all)
	buf := make([]byte, 4096)
	_ = dsx.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, from, err := dsx.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	got := string(buf[:n])
	for _, want := range []string{
		`{"type":1,"parameters":[0,1,22,2,5,4]}`,
		`{"type":1,"parameters":[0,2,22,2,5,6]}`,
		`{"type":2,"parameters":[0,0,0,0,0]}`,
		`{"type":6,"parameters":[0,1]}`,
		`{"type":5,"parameters":[0,2]}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}

	reply := `{"Status":"DSX Received UDP Instructions","isControllerConnected":true,"BatteryLevel":80,"Devices":[{"Index":0,"MacAddress":"aa","DeviceType":0,"ConnectionType":0,"BatteryLevel":80,"IsSupportAT":true,"IsSupportLightBar":true}]}`
	if _, err := dsx.WriteToUDP([]byte(reply), from); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); !c.Online() && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if !c.Online() {
		t.Fatal("DSX's answer not registered")
	}
	if got := c.Controllers(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("controllers %v", got)
	}
}

func TestChanges(t *testing.T) {
	f := weaponsFrame()
	all := Outputs{Triggers: true, Lightbar: true, PlayerLEDs: true, Mic: true}
	if list := changes(0, &f, f, all); len(list) != 0 {
		t.Fatalf("unchanged frame sent %v", list)
	}
	for _, in := range changes(0, nil, f, Outputs{Triggers: true}) {
		if in.Type != instTriggerUpdate {
			t.Fatalf("output not controlled was sent: %v", in)
		}
	}
	g := f
	g.Right = NewTrigger("OFF", nil)
	if list := changes(0, &f, g, all); len(list) != 1 || list[0].Parameters[2] != int(TriggerOff) {
		t.Fatalf("one trigger changed: %v", list)
	}
}

func TestMotionInstructions(t *testing.T) {
	b, _ := json.Marshal(packet{Instructions: motionInstructions([]int{0}, MotionDisabled)})
	if string(b) != `{"instructions":[{"type":8,"parameters":[0,2,7]}]}` {
		t.Fatalf("gyro off: %s", b)
	}
	b, _ = json.Marshal(packet{Instructions: motionInstructions([]int{0}, MotionNone)})
	if string(b) != `{"instructions":[{"type":8,"parameters":[0,2,0]}]}` {
		t.Fatalf("no mouse, motion passed on: %s", b)
	}
	b, _ = json.Marshal(packet{Instructions: motionInstructions([]int{0, 1}, MotionProfile)})
	if string(b) != `{"instructions":[{"type":8,"parameters":[0,-1,-1]},{"type":8,"parameters":[1,-1,-1]}]}` {
		t.Fatalf("gyro back to the profile: %s", b)
	}
}

func TestInstallProfile(t *testing.T) {
	dsx := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dsx, "DSX_Savefile", "Configuration Files", "Controller Profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if newestDSXFolder([]string{filepath.Join(t.TempDir(), "missing"), dsx}) != dsx {
		t.Fatal("DSX folder not found")
	}
	backups := filepath.Join(t.TempDir(), "backups")
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	exe := `D:\SteamLibrary\steamapps\common\Elite Dangerous\Products\elite-dangerous-odyssey-64\EliteDangerous64.exe`

	if did, err := installProfile(dsx, backups, exe, false, now); err != nil || did == "" {
		t.Fatalf("first install: %q %v", did, err)
	}
	if got, _ := os.ReadFile(profilePath(dsx)); string(got) != string(assets.DSXProfile) {
		t.Fatal("profile content")
	}
	raw, _ := os.ReadFile(gameProfilesPath(dsx))
	var games map[string]map[string]string
	if err := json.Unmarshal(raw, &games); err != nil || games[elite.SteamAppID]["ProfileName"] != ProfileName || games[elite.SteamAppID]["ExePath"] != exe {
		t.Fatalf("game profiles: %s %v", raw, err)
	}
	if !strings.Contains(string(raw), "\r\n") {
		t.Fatal("DSX's files use CRLF")
	}

	// the player's own profile and game profile are left alone
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(profilePath(dsx), "mine")
	write(gameProfilesPath(dsx), "\ufeff{\"359320\": {\"ProfileName\": \"My Elite\", \"ExePath\": \"x\", \"Other\": 1}, \"42\": {\"ProfileName\": \"Other game\"}}")
	if did, err := installProfile(dsx, backups, exe, false, now); err != nil || did != "" {
		t.Fatalf("existing profile touched: %q %v", did, err)
	}

	// reset: backup, overwrite, the game profile pointed at it, other fields kept
	if _, err := installProfile(dsx, backups, exe, true, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(profilePath(dsx)); string(got) != string(assets.DSXProfile) {
		t.Fatal("not reset")
	}
	saved, _ := filepath.Glob(filepath.Join(backups, ProfileName+"-*.dsx"))
	if len(saved) != 1 {
		t.Fatalf("backups %v", saved)
	}
	if b, _ := os.ReadFile(saved[0]); string(b) != "mine" {
		t.Fatal("backup content")
	}
	raw, _ = os.ReadFile(gameProfilesPath(dsx))
	if !strings.HasPrefix(string(raw), string(utf8BOM)) {
		t.Fatalf("the game profiles' BOM is gone: %q", raw)
	}
	var after map[string]map[string]any
	if err := json.Unmarshal(raw[len(utf8BOM):], &after); err != nil {
		t.Fatal(err)
	}
	e := after[elite.SteamAppID]
	if e["ProfileName"] != ProfileName || e["ExePath"] != "x" || e["Other"] != 1.0 || after["42"]["ProfileName"] != "Other game" {
		t.Fatalf("game profiles after reset: %s", raw)
	}
}

// TestInstallProfileLeaves: a profile that cannot be read is never
// replaced, nor one that appears right before the rename; a game profiles
// file holding null is not changed, and does not stop the profile.
func TestInstallProfileLeaves(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	for _, force := range []bool{false, true} {
		dir, backups := t.TempDir(), filepath.Join(t.TempDir(), "dsx_profile_backups")
		// a folder where the profile is: it cannot be read
		if err := os.MkdirAll(profilePath(dir), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := installProfile(dir, backups, "", force, now)
		if err == nil || !strings.Contains(err.Error(), "left as it is") {
			t.Errorf("force %v: %v", force, err)
		}
		if st, serr := os.Stat(profilePath(dir)); serr != nil || !st.IsDir() {
			t.Errorf("force %v: replaced", force)
		}
		if tmp, _ := filepath.Glob(filepath.Join(filepath.Dir(profilePath(dir)), "*.tmp")); len(tmp) != 0 {
			t.Errorf("force %v: %q left", force, tmp)
		}
	}

	// without replace, a file there right before the rename stays
	dir := t.TempDir()
	path := filepath.Join(dir, "x.dsx")
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("new"), false); !errors.Is(err, errThere) {
		t.Errorf("not replaced: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "mine" {
		t.Errorf("replaced: %q", b)
	}
	if tmp, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(tmp) != 0 {
		t.Errorf("%q left", tmp)
	}
	if err := writeFile(path, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("replace: %q", b)
	}

	// GameProfilesUpdates.json holding null
	dir = t.TempDir()
	games := gameProfilesPath(dir)
	if err := os.MkdirAll(filepath.Dir(games), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(games, []byte("\ufeffnull\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	did, err := installProfile(dir, filepath.Join(t.TempDir(), "b"), `C:\Elite\EliteDangerous64.exe`, true, now)
	if err != nil || !strings.Contains(did, "game profile not set: unreadable GameProfilesUpdates.json: it holds null") {
		t.Errorf("null: %q %v", did, err)
	}
	if b, _ := os.ReadFile(games); string(b) != "\ufeffnull\r\n" {
		t.Errorf("null changed: %q", b)
	}
	if !exists(profilePath(dir)) {
		t.Error("null: no profile")
	}
}

func TestProfileGyro(t *testing.T) {
	dsx := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dsx, "DSX_Savefile", "Configuration Files", "Controller Profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(gameProfilesPath(dsx)), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check := func(label string, mouse, known bool) {
		t.Helper()
		if m, k := profileGyro(dsx); m != mouse || k != known {
			t.Errorf("%s: mouse %v known %v, want %v %v", label, m, k, mouse, known)
		}
	}
	check("no game profiles", false, false)
	write(gameProfilesPath(dsx), "\ufeff{\"42\": {\"ProfileName\": \"Other game\"}}")
	check("no profile for Elite", false, false)
	write(gameProfilesPath(dsx), "\ufeff{\"359320\": {\"ProfileName\": \"My Elite\", \"ExePath\": \"x\"}}")
	check("the profile file missing", false, false)
	write(controllerProfilePath(dsx, "My Elite"), `{"controller_motion": {"motion_mode": "MOTION_TO_RIGHT_JOYSTICK"}}`)
	check("gyro as the right stick", false, true)
	write(controllerProfilePath(dsx, "My Elite"), "\ufeff{\"controller_motion\": {\"motion_mode\": \"MOTION_TO_MOUSE\"}}")
	check("motion to mouse", true, true)

	// the bundled profile is motion to mouse
	write(gameProfilesPath(dsx), `{"359320": {"ProfileName": "Elite Dangerous"}}`)
	write(profilePath(dsx), string(assets.DSXProfile))
	check("the bundled profile", true, true)
}

func TestProfileInUse(t *testing.T) {
	dsx := t.TempDir()
	dir := filepath.Join(dsx, "DSX_Savefile", "Configuration Files", "Controller Profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := profileInUse(dsx); got != "" {
		t.Errorf("no usage file: %q", got)
	}
	usage := `[{"profile_name": "Default Profile", "session_count": 14, "last_used_at": "2026-09-29T22:36:49.6385071+03:00"},
	{"profile_name": "Elite Dangerous", "session_count": 19, "last_used_at": "2026-09-29T20:56:44.4950601+03:00", "associated_game_ids": ["359320"]}]`
	if err := os.WriteFile(filepath.Join(dir, "profile_usage.json"), []byte("\ufeff"+usage), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := profileInUse(dsx); got != "Default Profile" {
		t.Errorf("in use: %q, want the one used last", got)
	}
	if got := eliteProfile(dsx); got != "" {
		t.Errorf("no game profiles: %q", got)
	}
}
