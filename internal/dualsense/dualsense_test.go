package dualsense

import (
	"bytes"
	"hash/crc32"
	"testing"
)

func TestRumbleReportUSB(t *testing.T) {
	b := RumbleReport(48, 10, 200)
	if len(b) != 48 || b[0] != 0x02 || b[1] != 0x03 || b[2] != 0 || b[3] != 200 || b[4] != 10 {
		t.Fatalf("USB report: % x", b[:8])
	}
	for i := 5; i < len(b); i++ {
		if b[i] != 0 {
			t.Fatalf("byte %d set: the report must not touch lightbar, triggers or LEDs", i)
		}
	}
}

func TestRumbleReportBluetooth(t *testing.T) {
	b := RumbleReport(78, 7, 9)
	if b[0] != 0x31 || b[2] != 0x03 || b[4] != 9 || b[5] != 7 {
		t.Fatalf("Bluetooth report: % x", b[:8])
	}
	c := crc32.NewIEEE()
	c.Write([]byte{0xA2})
	c.Write(b[:74])
	if v := c.Sum32(); b[74] != byte(v) || b[77] != byte(v>>24) {
		t.Fatal("CRC")
	}
}

func TestParseInputReport(t *testing.T) {
	usb := make([]byte, 64)
	usb[0] = 0x01
	usb[5], usb[6] = 30, 250 // L2, R2
	usb[8] = 0x40 | 0x08     // Circle, d-pad released
	usb[9] = 0x02 | 0x08     // R1, R2
	st, ok := ParseInputReport(usb)
	if !ok || st.L2 != 30 || st.R2 != 250 || !st.Held(Circle) || !st.Held(R1) || !st.Held(R2) || st.Held(Square) || st.Held(DpadUp) {
		t.Fatalf("USB: %+v", st)
	}
	usb[8] = 0x02
	if st, _ := ParseInputReport(usb); !st.Held(DpadRight) || st.Held(DpadUp) {
		t.Fatalf("d-pad right: %b", st.Buttons)
	}

	bt := make([]byte, 78)
	bt[0], bt[6], bt[10] = 0x31, 99, 0x01
	if st, ok := ParseInputReport(bt); !ok || st.L2 != 99 || !st.Held(L1) {
		t.Fatalf("Bluetooth: %+v", st)
	}

	// motion: gyro at bytes 16-21, accel 22-27, the sensor clock 28-31
	m := make([]byte, 64)
	m[0] = 0x01
	copy(m[16:], []byte{0x10, 0x00, 0xF0, 0xFF, 0x00, 0x80})
	copy(m[22:], []byte{0x00, 0x20, 0x01, 0x00, 0xFF, 0x7F})
	copy(m[28:], []byte{0x78, 0x56, 0x34, 0x12})
	if st, _ := ParseInputReport(m); st.Gyro != [3]int16{16, -16, -32768} || st.Accel != [3]int16{8192, 1, 32767} || st.Clock != 0x12345678 {
		t.Fatalf("motion: %+v", st)
	}
	if st, ok := ParseInputReport(m[:31]); !ok || st.Gyro[0] != 16 || st.Accel != [3]int16{} || st.Clock != 0 {
		t.Fatalf("a report too short for the clock: %+v", st)
	}
	copy(bt[23:], []byte{0xFE, 0xFF})
	copy(bt[29:], []byte{0x01, 0x00, 0x00, 0x80})
	if st, _ := ParseInputReport(bt); st.Accel[0] != -2 || st.Clock != 0x80000001 {
		t.Fatalf("Bluetooth motion: %+v", st)
	}
}

func TestParseTouch(t *testing.T) {
	b := make([]byte, 64)
	b[0] = 0x01
	b[33] = 0x80
	if st, _ := ParseInputReport(b); st.Touch {
		t.Fatal("contact bit 7 set means no touch")
	}
	b[33], b[34], b[35], b[36] = 0x05, 0x10, 0x32, 0x40
	if st, _ := ParseInputReport(b); !st.Touch {
		t.Fatal("touch not seen")
	}
	b[33], b[34], b[35], b[36] = 0, 0, 0, 0
	if st, _ := ParseInputReport(b); st.Touch {
		t.Fatal("all-zero touch data must not count")
	}
}

func TestClassify(t *testing.T) {
	path := `\\?\HID#VID_054C&PID_0CE6&MI_03#4&2dcc8cdc&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	if id := instanceID(path); id != `HID\VID_054C&PID_0CE6&MI_03\4&2dcc8cdc&0&0000` {
		t.Fatalf("instance id: %s", id)
	}
	usb := []string{`USB\VID_054C&PID_0CE6&MI_03\7&1&0003`, `USB\VID_054C&PID_0CE6\5&2`, `USB\ROOT_HUB30\4&3&0`, `PCI\VEN_8086&DEV_A36D\3&11583659&0&A0`, `ACPI\PNP0A08\0`, `ROOT\ACPI_HAL\0000`, `HTREE\ROOT\0`}
	virtual := []string{`USB\VID_054C&PID_0CE6&MI_03\4&1&0003`, `USB\VID_054C&PID_0CE6\1&2`, `ROOT\SYSTEM\0003`, `HTREE\ROOT\0`}
	for _, c := range []struct {
		path    string
		parents []string
		want    string
	}{
		{path, usb, Physical},
		{path, virtual, Virtual},
		{`\\?\hid#{00001124-0000-1000-8000-00805f9b34fb}_vid&0002054c_pid&0ce6#x`, nil, Physical},
		{path, nil, Unknown},
	} {
		if got := classify(c.path, c.parents); got != c.want {
			t.Errorf("%v: %s, want %s", c.parents, got, c.want)
		}
	}
}

// TestClassifyUSBIPWin2: DS4Windows' virtual pads hang under usbip-win2's
// virtual host controller, whose instance ID is only ROOT\USB\000N; its
// hardware ID or its driver tells it. The pads are virtual, and their USB
// parent is the Sony device above the interface.
func TestClassifyUSBIPWin2(t *testing.T) {
	path := `\?\HID#VID_054C&PID_0CE6&MI_03#8&1a2b3c&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	controller := ancestor{ID: `ROOT\USB\0000`, Hardware: []string{`ROOT\USBIP_WIN2\UDE`}, Service: "usbip2_ude"}
	viiper := func(c ancestor) []ancestor {
		return []ancestor{
			{ID: `USB\VID_054C&PID_0CE6&MI_03\7&2f4e&0&0003`, Hardware: []string{`USB\VID_054C&PID_0CE6&REV_0100&MI_03`, `USB\VID_054C&PID_0CE6&MI_03`}, Service: "HidUsb"},
			{ID: `USB\VID_054C&PID_0CE6\5&1c2d&0&1`, Hardware: []string{`USB\VID_054C&PID_0CE6&REV_0100`, `USB\VID_054C&PID_0CE6`}, Service: "usbccgp"},
			{ID: `USB\ROOT_HUB30\4&abc&0`, Hardware: []string{`USB\ROOT_HUB30&VID0000&PID0000&REV0000`, `USB\ROOT_HUB30`}, Service: "USBHUB3"},
			c,
			{ID: `HTREE\ROOT\0`},
		}
	}
	for _, c := range []struct {
		name string
		c    ancestor
	}{
		{"hardware ID and driver", controller},
		{"hardware ID", ancestor{ID: controller.ID, Hardware: []string{`root\usbip_win2\ude`}}},
		{"driver", ancestor{ID: controller.ID, Service: "USBIP2_UDE"}},
	} {
		kind, host := kindOf(path, viiper(c.c))
		if kind != Virtual || host != HostUSBIPWin2 {
			t.Errorf("%s: %s %q", c.name, kind, host)
		}
	}
	other := viiper(ancestor{ID: `ROOT\USB\0000`, Hardware: []string{`ROOT\OTHER_VHCI`}, Service: "othervhci"})
	if kind, host := kindOf(path, other); kind != Virtual || host != "" {
		t.Errorf("another root-enumerated USB controller: %s %q", kind, host)
	}
	if got := usbParent(ancestorIDs(viiper(controller))); got != `USB\VID_054C&PID_0CE6\5&1c2d&0&1` {
		t.Errorf("USB parent %q", got)
	}
	usb := []ancestor{{ID: `USB\VID_054C&PID_0CE6&MI_03\7&1&0003`}, {ID: `USB\VID_054C&PID_0CE6\5&2`}, {ID: `USB\ROOT_HUB30\4&3&0`, Service: "USBHUB3"},
		{ID: `PCI\VEN_8086&DEV_A36D\3&11583659&0&A0`, Hardware: []string{`PCI\VEN_8086&DEV_A36D`}, Service: "USBXHCI"}}
	if kind, host := kindOf(path, usb); kind != Physical || host != "" {
		t.Errorf("a wired pad: %s %q", kind, host)
	}
	if usbParent([]string{`ROOT\SYSTEM\0003`}) != "" {
		t.Error("USB parent without a Sony device")
	}
}

// TestPickVirtual: DS4Windows' pad under usbip-win2 comes first when asked
// for; a physical pad is never picked.
func TestPickVirtual(t *testing.T) {
	dsx := HIDDevice{Path: "dsx", ProductID: 0x0CE6, InLen: 64, OutLen: 48, Kind: Virtual}
	viiper := HIDDevice{Path: "viiper", ProductID: 0x0CE6, InLen: 64, OutLen: 48, Kind: Virtual, Host: HostUSBIPWin2}
	real := HIDDevice{Path: "real", ProductID: 0x0CE6, InLen: 64, OutLen: 48, Kind: Physical}
	ds4 := HIDDevice{Path: "ds4", ProductID: 0x09CC, InLen: 64, OutLen: 32, Kind: Virtual, Host: HostUSBIPWin2}
	for _, c := range []struct {
		devices []HIDDevice
		prefer  string
		want    string
	}{
		{[]HIDDevice{real, dsx, viiper}, "", "dsx"},
		{[]HIDDevice{real, dsx, viiper}, HostUSBIPWin2, "viiper"},
		{[]HIDDevice{real, ds4, dsx}, HostUSBIPWin2, "dsx"},
		{[]HIDDevice{real}, HostUSBIPWin2, ""},
	} {
		d, _ := pickVirtual(c.devices, c.prefer)
		if d.Path != c.want {
			t.Errorf("prefer %q: picked %q, want %q", c.prefer, d.Path, c.want)
		}
	}
}

// TestPickAudio: native haptics go to the controller's own audio device
// (wired) or to the virtual pad's, as asked.
func TestPickAudio(t *testing.T) {
	own := AudioDevice{ID: 1, Sony: true, Kind: Physical}
	viiper := AudioDevice{ID: 2, Sony: true, Kind: Virtual, Host: HostUSBIPWin2}
	dsx := AudioDevice{ID: 3, Sony: true, Kind: Virtual}
	speakers := AudioDevice{ID: 4, Kind: Physical}
	for _, c := range []struct {
		name    string
		devices []AudioDevice
		route   AudioRoute
		prefer  string
		want    int
	}{
		{"DSX", []AudioDevice{speakers, own, dsx}, RouteVirtual, "", 3},
		{"virtual, DS4Windows'", []AudioDevice{speakers, own, dsx, viiper}, RouteVirtual, HostUSBIPWin2, 2},
		{"controller", []AudioDevice{speakers, viiper, own}, RouteController, HostUSBIPWin2, 1},
		{"controller, not wired", []AudioDevice{speakers, viiper}, RouteController, HostUSBIPWin2, 0},
		{"auto, wired", []AudioDevice{speakers, viiper, own}, RouteAuto, HostUSBIPWin2, 1},
		{"auto, Bluetooth", []AudioDevice{speakers, viiper}, RouteAuto, HostUSBIPWin2, 2},
	} {
		d, _ := pickAudio(c.devices, c.route, c.prefer)
		if d.ID != c.want {
			t.Errorf("%s: device %d, want %d", c.name, d.ID, c.want)
		}
	}
}

// TestReleaseReport: the stop leaves both rumble bits off, as SDL's does,
// and touches nothing else. DSX's reports keep both bits.
func TestReleaseReport(t *testing.T) {
	b := ReleaseReport(48)
	if len(b) != 48 || b[0] != 0x02 {
		t.Fatalf("USB report: % x", b[:8])
	}
	for i := 1; i < len(b); i++ {
		if b[i] != 0 {
			t.Fatalf("byte %d set", i)
		}
	}
	bt := ReleaseReport(78)
	if len(bt) != 78 || bt[0] != 0x31 || bt[1] != 0x02 || bt[2] != 0 || bt[4] != 0 || bt[5] != 0 {
		t.Fatalf("Bluetooth report: % x", bt[:8])
	}
	c := crc32.NewIEEE()
	c.Write([]byte{0xA2})
	c.Write(bt[:74])
	if v := c.Sum32(); bt[74] != byte(v) || bt[75] != byte(v>>8) || bt[76] != byte(v>>16) || bt[77] != byte(v>>24) {
		t.Fatal("Bluetooth CRC")
	}
	if got := RumbleReport(48, 0, 0); got[1] != 0x03 {
		t.Fatal("DSX's stop changed")
	}
	if got := RumbleReport(48, 0, 200); got[1] != 0x03 || got[3] != 200 || got[4] != 0 {
		t.Fatalf("DSX's rumble changed: % x", got[:8])
	}
}

// TestLinkStopReport: with StopClears a stop is the release, and rumble
// stays as it is; without it the stop keeps both rumble bits.
func TestLinkStopReport(t *testing.T) {
	clears := LinkOptions{StopClears: true}
	if got := clears.report(48, 0, 0); !bytes.Equal(got, ReleaseReport(48)) {
		t.Errorf("stop: % x", got[:8])
	}
	if got := clears.report(48, 200, 0); !bytes.Equal(got, RumbleReport(48, 200, 0)) {
		t.Errorf("rumble: % x", got[:8])
	}
	if got := (LinkOptions{}).report(48, 0, 0); got[1] != 0x03 {
		t.Errorf("DSX's stop: % x", got[:8])
	}
}
