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
