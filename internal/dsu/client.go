package dsu

import (
	"errors"
	"math/rand/v2"
	"net"
	"sync"
	"time"
)

// Want is the controller EDSense drives, read by the client every Tick.
type Want struct {
	Active bool    // pad data is wanted now (the virtual DualSense is open); else only port info is asked for, and pad data the server still sends is dropped
	Slot   int     // its DS4Windows slot, from 0; -1: not known
	MAC    [6]byte // its MAC address; zero: not known
}

// Options set up a Client.
type Options struct {
	Addr    *net.UDPAddr     // the server; nil: ask nothing until Retarget
	Want    func() Want      // nil: always active, any slot
	Tick    time.Duration    // how often Want is read; 0: 250 ms
	Resend  time.Duration    // requests repeat this often; 0: 1 s (DS4Windows forgets a client after 5 s)
	Answers time.Duration    // a reply this recent means the server answers; 0: 3 s
	Fresh   time.Duration    // motion this recent means receiving; 0: 500 ms
	Now     func() time.Time // nil: time.Now
}

// State is what the client knows now.
type State struct {
	Addr      string // where it asks; "": nowhere
	Answers   bool   // a valid reply came within Answers
	Receiving bool   // motion of the picked slot came within Fresh
	NoData    bool   // its pad data has been asked for, and no motion came for Answers
	Slot      int    // the slot picked, from 0; -1: none
	Dropped   int    // datagrams dropped as malformed since New
}

// writeRedial: this many failed writes in a row dial the server again.
const writeRedial = 10

// seen is a slot's last port info, and when it came.
type seen struct {
	port Port
	at   time.Time
}

// Client asks a DSU server for the motion of the controller Want names. A
// reader goroutine per socket takes the replies; a sender goroutine asks
// for the ports and the pad data, again before the server forgets it.
type Client struct {
	o    Options
	id   uint32
	kick chan struct{}
	stop chan struct{}
	once sync.Once

	mu     sync.Mutex
	conn   *net.UDPConn // nil: asks nowhere
	addr   *net.UDPAddr
	closed bool
	onPad  func(Pad, time.Time)

	server     uint32
	haveServer bool
	ports      [Slots]seen
	counters   map[int]uint32 // the last accepted counter by slot
	want       Want           // as the sender last read it
	pick       int
	pickMAC    [6]byte
	lastReply  time.Time
	lastData   time.Time
	asking     time.Time // since when the picked slot's motion is waited for: the first data request, or the last motion; zero: not asked
	dropped    int

	// the sender's
	portsDue, dataDue   bool
	portsSent, dataSent time.Time
	writeErrs           int
}

// New dials Addr (udp4, connected, from an ephemeral port) and starts the
// goroutines.
func New(o Options) (*Client, error) {
	if o.Want == nil {
		o.Want = func() Want { return Want{Active: true, Slot: -1} }
	}
	if o.Tick <= 0 {
		o.Tick = 250 * time.Millisecond
	}
	if o.Resend <= 0 {
		o.Resend = time.Second
	}
	if o.Answers <= 0 {
		o.Answers = 3 * time.Second
	}
	if o.Fresh <= 0 {
		o.Fresh = 500 * time.Millisecond
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	c := &Client{o: o, id: rand.Uint32(), kick: make(chan struct{}, 1), stop: make(chan struct{}),
		counters: map[int]uint32{}, want: Want{Slot: -1}, pick: -1, portsDue: true}
	if o.Addr != nil {
		conn, err := net.DialUDP("udp4", nil, o.Addr)
		if err != nil {
			return nil, err
		}
		c.conn, c.addr = conn, o.Addr
		go c.read(conn)
	}
	go c.send()
	c.wake()
	return c, nil
}

// OnPad sets where the picked slot's data packets go, on the reader
// goroutine; nil stops them (a call under way may finish).
func (c *Client) OnPad(f func(p Pad, at time.Time)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onPad = f
}

// State is what the client knows now.
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.o.Now()
	s := State{Slot: c.pick, Dropped: c.dropped}
	if c.conn != nil {
		s.Addr = c.addr.String()
	}
	s.Answers = !c.lastReply.IsZero() && now.Sub(c.lastReply) < c.o.Answers
	s.Receiving = c.pick >= 0 && !c.lastData.IsZero() && now.Sub(c.lastData) < c.o.Fresh
	s.NoData = c.want.Active && c.pick >= 0 && !c.asking.IsZero() && now.Sub(c.asking) >= c.o.Answers
	return s
}

// Retarget asks addr from now on; nil stops asking. True when it changed.
func (c *Client) Retarget(addr *net.UDPAddr) (bool, error) {
	c.mu.Lock()
	same := c.closed || addr == nil && c.addr == nil ||
		addr != nil && c.addr != nil && addr.String() == c.addr.String()
	c.mu.Unlock()
	if same {
		return false, nil
	}
	return c.swap(addr, true)
}

// swap moves to a fresh socket for addr (nil: none), and forgets what the
// old server told when forget is set. The server sees a new client either
// way, so everything is asked again at once.
func (c *Client) swap(addr *net.UDPAddr, forget bool) (bool, error) {
	var conn *net.UDPConn
	if addr != nil {
		var err error
		if conn, err = net.DialUDP("udp4", nil, addr); err != nil {
			return false, err
		}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		if conn != nil {
			conn.Close()
		}
		return false, nil
	}
	old := c.conn
	c.conn, c.addr = conn, addr
	if forget {
		c.forget()
		c.lastReply, c.lastData = time.Time{}, time.Time{}
	}
	c.portsDue, c.dataDue, c.writeErrs = true, true, 0
	c.asking = time.Time{}
	c.mu.Unlock()
	if conn != nil {
		go c.read(conn)
	}
	if old != nil {
		old.Close()
	}
	c.wake()
	return true, nil
}

// forget drops what the server told: the port table, the counters, the
// pick and the server's id. Under mu.
func (c *Client) forget() {
	c.ports = [Slots]seen{}
	c.counters = map[int]uint32{}
	c.pick, c.pickMAC = -1, [6]byte{}
	c.haveServer = false
	c.asking = time.Time{}
}

// Close stops the client; it is safe to call again, and waits for nothing.
func (c *Client) Close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		conn := c.conn
		c.conn, c.addr = nil, nil
		c.mu.Unlock()
		close(c.stop)
		if conn != nil {
			conn.Close()
		}
	})
}

func (c *Client) wake() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// read takes the datagrams of one socket until it is closed.
func (c *Client) read(conn *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		n, err := conn.Read(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			// no server: the port unreachable comes back as a read error
			time.Sleep(100 * time.Millisecond)
			continue
		}
		c.take(conn, buf[:n])
	}
}

// take handles one datagram from conn.
func (c *Client) take(conn *net.UDPConn, b []byte) {
	m, err := Parse(b)
	c.mu.Lock()
	if conn != c.conn { // a socket Retarget left
		c.mu.Unlock()
		return
	}
	if err != nil {
		c.dropped++
		c.mu.Unlock()
		return
	}
	now := c.o.Now()
	c.lastReply = now
	if c.haveServer && m.Server != c.server {
		// a server started again has forgotten the subscription
		c.forget()
		c.portsDue, c.dataDue = true, true
		c.wake()
	}
	c.server, c.haveServer = m.Server, true
	switch m.Type {
	case MsgPorts:
		c.ports[m.Port.Slot] = seen{m.Port, now}
		c.repick(now)
	case MsgData:
		p := m.Pad
		// the server sends for a while after the last request: none of it
		// once the virtual DualSense has closed
		if !c.want.Active || c.pick < 0 || p.Slot != c.pick || c.pickMAC != [6]byte{} && p.MAC != c.pickMAC {
			c.mu.Unlock()
			return
		}
		if last, ok := c.counters[p.Slot]; ok && last == p.Counter {
			c.mu.Unlock()
			return // the same report again
		}
		c.counters[p.Slot] = p.Counter
		if p.HasMotion() {
			c.lastData, c.asking = now, now
		}
		f := c.onPad
		c.mu.Unlock()
		if f != nil {
			f(p, now)
		}
		return
	}
	c.mu.Unlock()
}

// repick picks the slot again from the fresh ports and the last Want; a
// new pick is asked for at once. Under mu.
func (c *Client) repick(now time.Time) {
	var fresh []Port
	for _, s := range c.ports {
		if !s.at.IsZero() && now.Sub(s.at) < c.o.Answers {
			fresh = append(fresh, s.port)
		}
	}
	pick, mac := Pick(fresh, c.want), [6]byte{}
	if pick >= 0 {
		mac = c.ports[pick].port.MAC
	}
	if pick != c.pick || mac != c.pickMAC {
		c.pick, c.pickMAC = pick, mac
		c.asking = time.Time{}
		if pick >= 0 {
			c.dataDue = true
			c.wake()
		}
	}
}

// send asks the server, every Tick and when woken.
func (c *Client) send() {
	tick := time.NewTicker(c.o.Tick)
	defer tick.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-tick.C:
		case <-c.kick:
		}
		w := c.o.Want() // never under mu: it asks other parts
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		now := c.o.Now()
		if w.Active && !c.want.Active {
			c.dataDue = true
		}
		c.want = w
		c.repick(now)
		if !w.Active || c.pick < 0 {
			c.asking = time.Time{}
		}
		conn := c.conn
		var out [][]byte
		if conn != nil {
			if c.portsDue || now.Sub(c.portsSent) >= c.o.Resend {
				out = append(out, PortsRequest(c.id, 0, 1, 2, 3))
				c.portsDue, c.portsSent = false, now
			}
			if c.want.Active && c.pick >= 0 && (c.dataDue || now.Sub(c.dataSent) >= c.o.Resend) {
				out = append(out, DataRequest(c.id, c.pick, [6]byte{}))
				c.dataDue, c.dataSent = false, now
				if c.asking.IsZero() {
					c.asking = now
				}
			}
		}
		c.mu.Unlock()
		failed := 0
		for _, b := range out {
			if _, err := conn.Write(b); err != nil && !errors.Is(err, net.ErrClosed) {
				failed++
			}
		}
		if len(out) > 0 {
			c.wrote(conn, failed)
		}
	}
}

// wrote counts writes that failed in a row on conn; too many dial the
// same server again.
func (c *Client) wrote(conn *net.UDPConn, failed int) {
	c.mu.Lock()
	if conn != c.conn {
		c.mu.Unlock()
		return
	}
	if failed == 0 {
		c.writeErrs = 0
		c.mu.Unlock()
		return
	}
	c.writeErrs += failed
	again := c.writeErrs >= writeRedial
	addr := c.addr
	c.mu.Unlock()
	if again {
		_, _ = c.swap(addr, false)
	}
}
