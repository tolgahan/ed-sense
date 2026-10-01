// Package dsu reads a DSU server, the motion protocol of emulators such as
// Cemu (cemuhook) that DS4Windows' UDP Server speaks: the controllers'
// motion as floats, with no dead band. It holds EDSense's own code,
// written from the protocol's public description and facts about
// DS4Windows' server (GPL-3, none of its code is here). It logs nothing.
package dsu

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"math"
	"strings"
)

const (
	DefaultPort = 26760 // DS4Windows' default
	Protocol    = 1001

	MsgVersion uint32 = 0x100000
	MsgPorts   uint32 = 0x100001
	MsgData    uint32 = 0x100002

	Slots = 4 // a DSU server has 4 slots

	StateDisconnected byte = 0
	StateReserved     byte = 1
	StateConnected    byte = 2

	headerLen = 16
	portsLen  = 32  // a port info packet
	dataLen   = 100 // a pad data packet
)

// Why Parse drops a datagram.
var (
	ErrShort   = errors.New("dsu: datagram too short")
	ErrMagic   = errors.New("dsu: not from a DSU server")
	ErrVersion = errors.New("dsu: another protocol version")
	ErrLength  = errors.New("dsu: wrong length")
	ErrCRC     = errors.New("dsu: wrong checksum")
	ErrValue   = errors.New("dsu: a value out of range")
)

// Port is one slot as a port info reply tells it.
type Port struct {
	Slot       int
	State      byte // StateDisconnected, StateReserved or StateConnected
	Model      byte
	Connection byte
	MAC        [6]byte
	Battery    byte
}

// Pad is one pad data packet.
type Pad struct {
	Slot    int
	State   byte
	MAC     [6]byte
	Counter uint32     // the controller's report counter
	Touch   bool       // the first touch is active, at a position other than 0,0
	Micros  uint64     // the motion clock, microseconds; 0: no motion
	Accel   [3]float32 // g, X Y Z, as sent
	Gyro    [3]float32 // deg/s: pitch, yaw, roll, as sent
}

// HasMotion: the packet carries motion (a server sends zeros without it).
func (p Pad) HasMotion() bool {
	return p.Micros != 0 || p.Accel != [3]float32{} || p.Gyro != [3]float32{}
}

// Message is one reply.
type Message struct {
	Type   uint32
	Server uint32 // the server's id; a new one after a restart
	Port   Port   // for MsgPorts
	Pad    Pad    // for MsgData
}

// request is a client packet of type with payload, its checksum made.
func request(client, typ uint32, payload []byte) []byte {
	b := make([]byte, headerLen+4+len(payload))
	copy(b, "DSUC")
	binary.LittleEndian.PutUint16(b[4:], Protocol)
	binary.LittleEndian.PutUint16(b[6:], uint16(len(b)-headerLen))
	binary.LittleEndian.PutUint32(b[12:], client)
	binary.LittleEndian.PutUint32(b[16:], typ)
	copy(b[20:], payload)
	binary.LittleEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b))
	return b
}

// PortsRequest asks for the port info of slots (at most 4 are asked).
func PortsRequest(client uint32, slots ...byte) []byte {
	if len(slots) > Slots {
		slots = slots[:Slots]
	}
	payload := make([]byte, 4+len(slots))
	binary.LittleEndian.PutUint32(payload, uint32(len(slots)))
	copy(payload[4:], slots)
	return request(client, MsgPorts, payload)
}

// DataRequest subscribes to pad data: of slot when slot >= 0, else of the
// controller with mac when mac is not zero, else of every slot.
func DataRequest(client uint32, slot int, mac [6]byte) []byte {
	payload := make([]byte, 8)
	switch {
	case slot >= 0:
		payload[0], payload[1] = 0x01, byte(slot)
	case mac != [6]byte{}:
		payload[0] = 0x02
		copy(payload[2:], mac[:])
	}
	return request(client, MsgData, payload)
}

// Parse reads one datagram from a server.
func Parse(b []byte) (Message, error) {
	if len(b) < headerLen+4 {
		return Message{}, ErrShort
	}
	if string(b[:4]) != "DSUS" {
		return Message{}, ErrMagic
	}
	if binary.LittleEndian.Uint16(b[4:]) != Protocol {
		return Message{}, ErrVersion
	}
	l := int(binary.LittleEndian.Uint16(b[6:]))
	if headerLen+l > len(b) || l < 4 {
		return Message{}, ErrLength
	}
	b = b[:headerLen+l] // trailing bytes do not count
	// the checksum is over the packet with its own field zeroed; a copy,
	// so the caller's bytes stay as they are
	c := make([]byte, len(b))
	copy(c, b)
	clear(c[8:12])
	if binary.LittleEndian.Uint32(b[8:]) != crc32.ChecksumIEEE(c) {
		return Message{}, ErrCRC
	}
	m := Message{Server: binary.LittleEndian.Uint32(b[12:]), Type: binary.LittleEndian.Uint32(b[16:])}
	switch m.Type {
	case MsgPorts:
		if len(b) < portsLen {
			return Message{}, ErrLength
		}
		if b[20] >= Slots {
			return Message{}, ErrValue
		}
		m.Port = Port{Slot: int(b[20]), State: b[21], Model: b[22], Connection: b[23], Battery: b[30]}
		copy(m.Port.MAC[:], b[24:30])
	case MsgData:
		if len(b) < dataLen {
			return Message{}, ErrLength
		}
		if b[20] >= Slots {
			return Message{}, ErrValue
		}
		p := Pad{Slot: int(b[20]), State: b[21], Counter: binary.LittleEndian.Uint32(b[32:]), Micros: binary.LittleEndian.Uint64(b[68:])}
		copy(p.MAC[:], b[24:30])
		x, y := binary.LittleEndian.Uint16(b[58:]), binary.LittleEndian.Uint16(b[60:])
		p.Touch = b[56] != 0 && (x != 0 || y != 0)
		for i := range 3 {
			p.Accel[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[76+4*i:]))
			p.Gyro[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[88+4*i:]))
			for _, v := range [2]float32{p.Accel[i], p.Gyro[i]} {
				if f := float64(v); math.IsNaN(f) || math.IsInf(f, 0) {
					return Message{}, ErrValue
				}
			}
		}
		m.Pad = p
	}
	return m, nil
}

// ParseMAC reads "AA:BB:CC:DD:EE:FF" (also with '-' or no separators);
// false for anything else and for all zeros.
func ParseMAC(s string) ([6]byte, bool) {
	var mac [6]byte
	s = strings.TrimSpace(s)
	if len(s) == 17 {
		sep := s[2]
		if sep != ':' && sep != '-' {
			return mac, false
		}
		for i := 2; i < len(s); i += 3 {
			if s[i] != sep {
				return mac, false
			}
		}
		s = strings.ReplaceAll(s, string(sep), "")
	}
	if len(s) != 12 {
		return mac, false
	}
	if _, err := hex.Decode(mac[:], []byte(s)); err != nil {
		return [6]byte{}, false
	}
	return mac, mac != [6]byte{}
}

// Pick is the slot to read among the fresh ports for w; -1 for none. With
// a known MAC only that controller is read, and with a known slot only
// that slot, so another controller's motion never moves the aim. Only
// when neither is known is the lowest connected slot read.
func Pick(ports []Port, w Want) int {
	var on []Port
	for _, p := range ports {
		if p.State == StateConnected && p.Slot >= 0 && p.Slot < Slots {
			on = append(on, p)
		}
	}
	if w.MAC != [6]byte{} {
		for _, p := range on {
			if p.MAC == w.MAC {
				return p.Slot
			}
		}
		return -1
	}
	lowest := -1
	for _, p := range on {
		if p.Slot == w.Slot {
			return p.Slot
		}
		if lowest < 0 || p.Slot < lowest {
			lowest = p.Slot
		}
	}
	if w.Slot >= 0 {
		// DS4Windows calls a controller with a blank serial disconnected,
		// and serves controllers 1 to 4 only
		return -1
	}
	return lowest
}
