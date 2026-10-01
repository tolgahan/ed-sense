// Package dsutest is a DSU server for tests, on 127.0.0.1 with a random
// port: it answers port info requests from its own table, keeps the
// subscriptions as DS4Windows does, and sends the pad data a test hands
// it.
package dsutest

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/dsu"
)

// Server is a DSU server on 127.0.0.1. Its settings are set through its
// methods, which the server's goroutine reads under its lock.
type Server struct {
	conn *net.UDPConn
	done chan struct{}

	mu      sync.Mutex
	id      uint32
	ports   [dsu.Slots]dsu.Port
	forget  time.Duration
	silent  bool
	nPorts  int
	nData   int
	clients map[string]*client
	closed  bool
}

// client is one client the server has seen, and what it asked for.
type client struct {
	addr  *net.UDPAddr
	all   time.Time
	slots [dsu.Slots]time.Time
	macs  map[[6]byte]time.Time
}

// NewServer listens on 127.0.0.1 with a random port until the test ends.
func NewServer(t testing.TB) *Server {
	t.Helper()
	s, err := Listen(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// Listen is NewServer at addr, for a server started again on a port; the
// caller closes it.
func Listen(addr *net.UDPAddr) (*Server, error) {
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{conn: conn, done: make(chan struct{}), id: 0xAABBCCDD, forget: 5 * time.Second, clients: map[string]*client{}}
	go s.serve()
	return s, nil
}

// Addr is where it listens.
func (s *Server) Addr() *net.UDPAddr { return s.conn.LocalAddr().(*net.UDPAddr) }

// SetID sets the server's id for the replies from now on.
func (s *Server) SetID(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = id
}

// SetPorts sets what the port info replies tell.
func (s *Server) SetPorts(p [dsu.Slots]dsu.Port) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ports = p
}

// SetForget sets how long a subscription lasts; 5 s at first, as
// DS4Windows keeps it.
func (s *Server) SetForget(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forget = d
}

// SetSilent: the server answers nothing, and sends no pad data.
func (s *Server) SetSilent(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.silent = on
}

// Requests counts the valid port info and pad data requests it got.
func (s *Server) Requests() (ports, data int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nPorts, s.nData
}

// Subscribed: a client's subscription covers slot now.
func (s *Server) Subscribed(slot int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, c := range s.clients {
		if s.live(c.all, now) || slot >= 0 && slot < dsu.Slots && s.live(c.slots[slot], now) {
			return true
		}
	}
	return false
}

// live: a request made at is still in force at now. Under mu.
func (s *Server) live(at, now time.Time) bool { return !at.IsZero() && now.Sub(at) < s.forget }

// Send sends one pad data packet to every client whose subscription covers
// p.Slot or p.MAC, or all slots.
func (s *Server) Send(p dsu.Pad) {
	s.mu.Lock()
	if s.silent || s.closed {
		s.mu.Unlock()
		return
	}
	b := DataReply(s.id, p)
	now := time.Now()
	var to []*net.UDPAddr
	for _, c := range s.clients {
		if s.live(c.all, now) || p.Slot >= 0 && p.Slot < dsu.Slots && s.live(c.slots[p.Slot], now) ||
			p.MAC != [6]byte{} && s.live(c.macs[p.MAC], now) {
			to = append(to, c.addr)
		}
	}
	s.mu.Unlock()
	for _, a := range to {
		_, _ = s.conn.WriteToUDP(b, a)
	}
}

// Raw sends b as it is to every client seen.
func (s *Server) Raw(b []byte) {
	s.mu.Lock()
	var to []*net.UDPAddr
	for _, c := range s.clients {
		to = append(to, c.addr)
	}
	s.mu.Unlock()
	for _, a := range to {
		_, _ = s.conn.WriteToUDP(b, a)
	}
}

// Close stops the server and waits for its goroutine.
func (s *Server) Close() {
	s.mu.Lock()
	already := s.closed
	s.closed = true
	s.mu.Unlock()
	if already {
		return
	}
	s.conn.Close()
	<-s.done
}

func (s *Server) serve() {
	defer close(s.done)
	buf := make([]byte, 2048)
	for {
		n, from, err := s.conn.ReadFromUDP(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			continue
		}
		s.handle(buf[:n], from)
	}
}

// handle answers one request, read as strictly as DS4Windows reads it.
func (s *Server) handle(b []byte, from *net.UDPAddr) {
	typ, payload, ok := readRequest(b)
	if !ok {
		return
	}
	s.mu.Lock()
	if s.silent || s.closed {
		s.mu.Unlock()
		return
	}
	c := s.clients[from.String()]
	if c == nil {
		c = &client{addr: from, macs: map[[6]byte]time.Time{}}
		s.clients[from.String()] = c
	}
	var replies [][]byte
	switch typ {
	case dsu.MsgPorts:
		if len(payload) < 4 {
			break
		}
		n := int(int32(binary.LittleEndian.Uint32(payload)))
		if n < 0 || n > dsu.Slots || len(payload) < 4+n {
			break
		}
		slots := payload[4 : 4+n]
		valid := true
		for _, slot := range slots {
			valid = valid && slot < dsu.Slots
		}
		if !valid {
			break
		}
		s.nPorts++
		for _, slot := range slots {
			replies = append(replies, PortReply(s.id, s.ports[slot]))
		}
	case dsu.MsgData:
		if len(payload) < 8 {
			break
		}
		s.nData++
		now := time.Now()
		switch flags := payload[0]; {
		case flags == 0:
			c.all = now
		default:
			if flags&0x01 != 0 && payload[1] < dsu.Slots {
				c.slots[payload[1]] = now
			}
			if flags&0x02 != 0 {
				var mac [6]byte
				copy(mac[:], payload[2:8])
				c.macs[mac] = now
			}
		}
	}
	s.mu.Unlock()
	for _, r := range replies {
		_, _ = s.conn.WriteToUDP(r, from)
	}
}

// readRequest checks a client packet: its magic, a version up to 1001, an
// exact length and its checksum. It returns the type and what follows it.
func readRequest(b []byte) (uint32, []byte, bool) {
	if len(b) < 20 || string(b[:4]) != "DSUC" || binary.LittleEndian.Uint16(b[4:]) > dsu.Protocol {
		return 0, nil, false
	}
	if int(binary.LittleEndian.Uint16(b[6:]))+16 != len(b) {
		return 0, nil, false
	}
	c := append([]byte(nil), b...)
	clear(c[8:12])
	if binary.LittleEndian.Uint32(b[8:]) != crc32.ChecksumIEEE(c) {
		return 0, nil, false
	}
	return binary.LittleEndian.Uint32(b[16:]), b[20:], true
}

// reply is a server packet of n bytes, with typ and the server's id; fill
// writes the rest, and the checksum is made last.
func reply(server, typ uint32, n int, fill func(b []byte)) []byte {
	b := make([]byte, n)
	copy(b, "DSUS")
	binary.LittleEndian.PutUint16(b[4:], dsu.Protocol)
	binary.LittleEndian.PutUint16(b[6:], uint16(n-16))
	binary.LittleEndian.PutUint32(b[12:], server)
	binary.LittleEndian.PutUint32(b[16:], typ)
	fill(b)
	binary.LittleEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b))
	return b
}

// PortReply is the 32-byte port info packet for p.
func PortReply(server uint32, p dsu.Port) []byte {
	return reply(server, dsu.MsgPorts, 32, func(b []byte) {
		b[20], b[21], b[22], b[23] = byte(p.Slot), p.State, p.Model, p.Connection
		copy(b[24:30], p.MAC[:])
		b[30] = p.Battery
	})
}

// DataReply is the 100-byte pad data packet for p: connected over USB
// (model 2, connection 1), battery full and active, sticks centred, the
// first touch at 1,1 when p.Touch.
func DataReply(server uint32, p dsu.Pad) []byte {
	return reply(server, dsu.MsgData, 100, func(b []byte) {
		b[20], b[21], b[22], b[23] = byte(p.Slot), p.State, 2, 1
		copy(b[24:30], p.MAC[:])
		b[30], b[31] = 5, 1
		binary.LittleEndian.PutUint32(b[32:], p.Counter)
		b[40], b[41], b[42], b[43] = 0x80, 0x7F, 0x80, 0x7F
		if p.Touch {
			b[56] = 1
			binary.LittleEndian.PutUint16(b[58:], 1)
			binary.LittleEndian.PutUint16(b[60:], 1)
		}
		binary.LittleEndian.PutUint64(b[68:], p.Micros)
		for i := range 3 {
			binary.LittleEndian.PutUint32(b[76+4*i:], math.Float32bits(p.Accel[i]))
			binary.LittleEndian.PutUint32(b[88+4*i:], math.Float32bits(p.Gyro[i]))
		}
	})
}
