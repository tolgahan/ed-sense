package dsu_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"math"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/dsu"
	"github.com/tolgahan/ed-sense/internal/dsu/dsutest"
)

// unhex reads bytes written as hex pairs, with any spaces.
func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var mac = [6]byte{0x0A, 0x1B, 0x2C, 0x3D, 0x4E, 0x5F}

// The vectors, made with Python from DS4Windows' packet layout, the
// checksum by zlib.crc32.
const (
	portsVector  = "44 53 55 43 E9 03 0C 00 3D B2 4D 28 78 56 34 12 01 00 10 00 04 00 00 00 00 01 02 03"
	bySlotVector = "44 53 55 43 E9 03 0C 00 A6 37 CE BD 78 56 34 12 02 00 10 00 01 00 00 00 00 00 00 00"
	byMACVector  = "44 53 55 43 E9 03 0C 00 1B FD 93 D6 78 56 34 12 02 00 10 00 02 00 0A 1B 2C 3D 4E 5F"
	portVector   = "44 53 55 53 E9 03 10 00 52 3D 40 0D DD CC BB AA 01 00 10 00 00 02 02 01 0A 1B 2C 3D 4E 5F 05 00"
	dataVector   = `
44 53 55 53 E9 03 54 00 E3 85 24 69 DD CC BB AA
02 00 10 00 00 02 02 01 0A 1B 2C 3D 4E 5F 05 01
E8 03 00 00 00 00 00 00 80 7F 80 7F 00 00 00 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00
00 00 00 00 15 CD 5B 07 00 00 00 00 00 00 4D BD
00 B0 7E BF 00 C0 19 3E 00 00 20 41 00 00 C8 41
00 00 40 C0`
)

// vectorPad is the pad data packet of the vector.
var vectorPad = dsu.Pad{Slot: 0, State: dsu.StateConnected, MAC: mac, Counter: 1000, Micros: 123456789,
	Accel: [3]float32{-0.050048828125, -0.994873046875, 0.150146484375}, Gyro: [3]float32{10, 25, -3}}

func TestRequests(t *testing.T) {
	const client = 0x12345678
	for _, c := range []struct {
		name string
		got  []byte
		want string
	}{
		{"list ports 0-3", dsu.PortsRequest(client, 0, 1, 2, 3), portsVector},
		{"pad data by slot 0", dsu.DataRequest(client, 0, mac), bySlotVector},
		{"pad data by MAC", dsu.DataRequest(client, -1, mac), byMACVector},
	} {
		if want := unhex(t, c.want); !bytes.Equal(c.got, want) {
			t.Errorf("%s:\n got % X\nwant % X", c.name, c.got, want)
		}
	}
	all := dsu.DataRequest(client, -1, [6]byte{})
	if len(all) != 28 || !bytes.Equal(all[20:], make([]byte, 8)) {
		t.Errorf("pad data of every slot: % X", all)
	}
	if got := dsu.PortsRequest(client, 0, 1, 2, 3, 0); len(got) != 28 {
		t.Errorf("five slots asked: %d bytes", len(got))
	}
}

func TestParseData(t *testing.T) {
	b := unhex(t, dataVector)
	if got := dsutest.DataReply(0xAABBCCDD, vectorPad); !bytes.Equal(got, b) {
		t.Fatalf("DataReply:\n got % X\nwant % X", got, b)
	}
	m, err := dsu.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != dsu.MsgData || m.Server != 0xAABBCCDD || m.Pad != vectorPad {
		t.Errorf("parsed %+v", m)
	}
	if !m.Pad.HasMotion() {
		t.Error("the vector has motion")
	}
	if (dsu.Pad{}).HasMotion() || !(dsu.Pad{Micros: 1}).HasMotion() || !(dsu.Pad{Gyro: [3]float32{0, 0, 0.5}}).HasMotion() {
		t.Error("HasMotion")
	}
	// the touch: active and away from 0,0
	for _, c := range []struct {
		active byte
		x, y   uint16
		want   bool
	}{{1, 0, 0, false}, {1, 300, 0, true}, {1, 0, 20, true}, {0, 300, 200, false}} {
		tb := append([]byte(nil), b...)
		tb[56] = c.active
		binary.LittleEndian.PutUint16(tb[58:], c.x)
		binary.LittleEndian.PutUint16(tb[60:], c.y)
		fixCRC(tb)
		m, err := dsu.Parse(tb)
		if err != nil || m.Pad.Touch != c.want {
			t.Errorf("touch %d at %d,%d: %v %v", c.active, c.x, c.y, m.Pad.Touch, err)
		}
	}
}

func TestParsePorts(t *testing.T) {
	b := unhex(t, portVector)
	p := dsu.Port{Slot: 0, State: dsu.StateConnected, Model: 2, Connection: 1, MAC: mac, Battery: 5}
	if got := dsutest.PortReply(0xAABBCCDD, p); !bytes.Equal(got, b) {
		t.Fatalf("PortReply:\n got % X\nwant % X", got, b)
	}
	m, err := dsu.Parse(b)
	if err != nil || m.Type != dsu.MsgPorts || m.Server != 0xAABBCCDD || m.Port != p {
		t.Errorf("parsed %+v, %v", m, err)
	}
}

// fixCRC makes the packet's checksum again.
func fixCRC(b []byte) {
	clear(b[8:12])
	binary.LittleEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b))
}

func TestCRC(t *testing.T) {
	b := unhex(t, dataVector)
	keep := append([]byte(nil), b...)
	if _, err := dsu.Parse(b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, keep) {
		t.Fatal("Parse changed the caller's bytes")
	}
	// a flipped byte of the magic, the version or the length fails before
	// the checksum; any other one fails it, and passes once it is made
	// again
	for i := range b {
		if i >= 8 && i < 12 {
			continue
		}
		f := append([]byte(nil), b...)
		f[i] ^= 0x01
		_, err := dsu.Parse(f)
		switch {
		case err == nil:
			t.Errorf("byte %d flipped: parsed", i)
		case i >= 12 && !errors.Is(err, dsu.ErrCRC):
			t.Errorf("byte %d flipped: %v, want a wrong checksum", i, err)
		}
		if i >= 12 {
			fixCRC(f)
			if _, err := dsu.Parse(f); err != nil {
				t.Errorf("byte %d flipped, checksum made again: %v", i, err)
			}
		}
	}
	f := append([]byte(nil), b...)
	f[33] = 0x99 // the counter
	fixCRC(f)
	if m, err := dsu.Parse(f); err != nil || m.Pad.Counter != 0x99E8 {
		t.Errorf("counter changed, checksum made again: %+v %v", m.Pad, err)
	}
}

func TestMalformed(t *testing.T) {
	data := unhex(t, dataVector)
	ports := unhex(t, portVector)
	edit := func(b []byte, f func(b []byte)) []byte {
		b = append([]byte(nil), b...)
		f(b)
		fixCRC(b)
		return b
	}
	short := func(b []byte, n int) []byte {
		return edit(b[:n], func(b []byte) { binary.LittleEndian.PutUint16(b[6:], uint16(n-16)) })
	}
	nan, inf := math.Float32bits(float32(math.NaN())), math.Float32bits(float32(math.Inf(1)))
	for _, c := range []struct {
		name string
		b    []byte
		want error
	}{
		{"nil", nil, dsu.ErrShort},
		{"19 bytes", data[:19], dsu.ErrShort},
		{"a client's magic", edit(data, func(b []byte) { copy(b, "DSUC") }), dsu.ErrMagic},
		{"version 1000", edit(data, func(b []byte) { binary.LittleEndian.PutUint16(b[4:], 1000) }), dsu.ErrVersion},
		{"version 1002", edit(data, func(b []byte) { binary.LittleEndian.PutUint16(b[4:], 1002) }), dsu.ErrVersion},
		{"length past the end", edit(data, func(b []byte) { binary.LittleEndian.PutUint16(b[6:], 85) }), dsu.ErrLength},
		{"length under 4", edit(data, func(b []byte) { binary.LittleEndian.PutUint16(b[6:], 3) }), dsu.ErrLength},
		{"ports under 32", short(ports, 31), dsu.ErrLength},
		{"data under 100", short(data, 99), dsu.ErrLength},
		{"ports of slot 4", edit(ports, func(b []byte) { b[20] = 4 }), dsu.ErrValue},
		{"data of slot 4", edit(data, func(b []byte) { b[20] = 4 }), dsu.ErrValue},
		{"NaN gyro", edit(data, func(b []byte) { binary.LittleEndian.PutUint32(b[92:], nan) }), dsu.ErrValue},
		{"+Inf accel", edit(data, func(b []byte) { binary.LittleEndian.PutUint32(b[84:], inf) }), dsu.ErrValue},
		{"trailing bytes", append(append([]byte(nil), data...), 1, 2, 3), nil},
	} {
		_, err := dsu.Parse(c.b)
		if !errors.Is(err, c.want) || c.want == nil && err != nil {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// a version reply, and a type the client does not know
	for _, typ := range []uint32{dsu.MsgVersion, 0x100009} {
		b := edit(make([]byte, 24), func(b []byte) {
			copy(b, "DSUS")
			binary.LittleEndian.PutUint16(b[4:], dsu.Protocol)
			binary.LittleEndian.PutUint16(b[6:], 8)
			binary.LittleEndian.PutUint32(b[12:], 7)
			binary.LittleEndian.PutUint32(b[16:], typ)
		})
		m, err := dsu.Parse(b)
		if err != nil || m != (dsu.Message{Type: typ, Server: 7}) {
			t.Errorf("type %#x: %+v %v", typ, m, err)
		}
	}
}

func TestParseMAC(t *testing.T) {
	for _, c := range []struct {
		s  string
		ok bool
	}{
		{"0A:1B:2C:3D:4E:5F", true},
		{"0A-1B-2C-3D-4E-5F", true},
		{"0A1B2C3D4E5F", true},
		{"0a:1b:2c:3d:4e:5f", true},
		{" 0A:1B:2C:3D:4E:5F ", true},
		{"00:00:00:00:00:00", false},
		{"0A:1B:2C:3D:4E", false},
		{"0A:1B-2C:3D:4E:5F", false},
		{"0A:1B:2C:3D:4E:5G", false},
		{"not a mac", false},
		{"", false},
	} {
		got, ok := dsu.ParseMAC(c.s)
		if ok != c.ok || ok && got != mac {
			t.Errorf("%q: % X %v", c.s, got, ok)
		}
	}
}

func TestPick(t *testing.T) {
	other := [6]byte{1, 2, 3, 4, 5, 6}
	on := func(slot int, m [6]byte) dsu.Port { return dsu.Port{Slot: slot, State: dsu.StateConnected, MAC: m} }
	off := func(slot int) dsu.Port { return dsu.Port{Slot: slot, State: dsu.StateDisconnected} }
	for _, c := range []struct {
		name  string
		ports []dsu.Port
		w     dsu.Want
		want  int
	}{
		{"one connected", []dsu.Port{on(0, mac), off(1), off(2), off(3)}, dsu.Want{Slot: -1}, 0},
		{"none", []dsu.Port{off(0), off(1), off(2), off(3)}, dsu.Want{Slot: 0}, -1},
		{"reserved only", []dsu.Port{{Slot: 0, State: dsu.StateReserved, MAC: mac}}, dsu.Want{Slot: 0}, -1},
		{"two, by MAC", []dsu.Port{on(0, other), on(1, mac)}, dsu.Want{Slot: 0, MAC: mac}, 1},
		{"two, MAC of neither", []dsu.Port{on(0, other), on(1, other)}, dsu.Want{Slot: 0, MAC: mac}, -1},
		{"one, another MAC", []dsu.Port{on(0, other)}, dsu.Want{Slot: 0, MAC: mac}, -1},
		{"MAC unknown, by slot", []dsu.Port{on(0, other), on(2, mac)}, dsu.Want{Slot: 2}, 2},
		// a known slot that is not connected is never another one: a blank
		// serial makes DS4Windows call the controller disconnected
		{"MAC unknown, slot disconnected", []dsu.Port{off(0), on(2, mac), on(3, other)}, dsu.Want{Slot: 0}, -1},
		{"MAC unknown, slot 5", []dsu.Port{on(3, mac), on(1, other)}, dsu.Want{Slot: 5}, -1},
		{"slot and MAC unknown", []dsu.Port{off(0), on(3, mac), on(1, other)}, dsu.Want{Slot: -1}, 1},
		{"no ports", nil, dsu.Want{Slot: -1}, -1},
	} {
		if got := dsu.Pick(c.ports, c.w); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}
