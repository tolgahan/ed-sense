package dualsense

import (
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
