package dsx

import (
	"encoding/json"
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
	b, _ := json.Marshal(packet{Instructions: motionInstructions([]int{0}, true)})
	if string(b) != `{"instructions":[{"type":8,"parameters":[0,2,7]}]}` {
		t.Fatalf("gyro off: %s", b)
	}
	b, _ = json.Marshal(packet{Instructions: motionInstructions([]int{0, 1}, false)})
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
	exe := `D:\SteamLibrary\steamapps\common\Elite Dangerous\Products\elite-dangerous-odyssey-64\EliteDangerous64.exe`

	if did, err := installProfile(dsx, backups, exe, false); err != nil || did == "" {
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
	if did, err := installProfile(dsx, backups, exe, false); err != nil || did != "" {
		t.Fatalf("existing profile touched: %q %v", did, err)
	}

	// reset: backup, overwrite, the game profile pointed at it, other fields kept
	if _, err := installProfile(dsx, backups, exe, true); err != nil {
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
	var after map[string]map[string]any
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	e := after[elite.SteamAppID]
	if e["ProfileName"] != ProfileName || e["ExePath"] != "x" || e["Other"] != 1.0 || after["42"]["ProfileName"] != "Other game" {
		t.Fatalf("game profiles after reset: %s", raw)
	}
}
