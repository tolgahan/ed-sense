package dsx

import (
	"net"
	"strings"
	"testing"
	"time"
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
