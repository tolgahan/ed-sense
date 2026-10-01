package dsu_test

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/dsu"
	"github.com/tolgahan/ed-sense/internal/dsu/dsutest"
)

// rig is a client of a fake server, fast: it reads Want every 10 ms, asks
// again every 40 ms, and the server forgets it after 150 ms.
type rig struct {
	t       *testing.T
	srv     *dsutest.Server
	c       *dsu.Client
	want    atomic.Pointer[dsu.Want]
	counter atomic.Uint32

	mu  sync.Mutex
	got []dsu.Pad
	at  []time.Time
}

// later is the clock the client is given: an hour ahead, so its times are
// told apart from the wall clock's.
func later() time.Time { return time.Now().Add(time.Hour) }

func newRig(t *testing.T, w dsu.Want, edit func(o *dsu.Options)) *rig {
	r := &rig{t: t, srv: dsutest.NewServer(t)}
	r.srv.SetForget(150 * time.Millisecond)
	r.srv.SetPorts(ports(0, mac))
	r.want.Store(&w)
	o := dsu.Options{Addr: r.srv.Addr(), Want: func() dsu.Want { return *r.want.Load() },
		Tick: 10 * time.Millisecond, Resend: 40 * time.Millisecond, Answers: 200 * time.Millisecond,
		Fresh: 60 * time.Millisecond, Now: later}
	if edit != nil {
		edit(&o)
	}
	c, err := dsu.New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	r.c = c
	c.OnPad(r.take)
	return r
}

func (r *rig) take(p dsu.Pad, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got, r.at = append(r.got, p), append(r.at, at)
}

func (r *rig) received() []dsu.Pad {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]dsu.Pad(nil), r.got...)
}

func (r *rig) setWant(w dsu.Want) { r.want.Store(&w) }

// pad is a packet of slot with motion and a new counter.
func (r *rig) pad(slot int, m [6]byte) dsu.Pad {
	return dsu.Pad{Slot: slot, State: dsu.StateConnected, MAC: m, Counter: r.counter.Add(1), Micros: 5000 + uint64(r.counter.Load()),
		Accel: [3]float32{0, 1, 0}, Gyro: [3]float32{1.5, -0.25, 0}}
}

// ports is a table with the controller mac connected in slot.
func ports(slot int, m [6]byte) [dsu.Slots]dsu.Port {
	var p [dsu.Slots]dsu.Port
	for i := range p {
		p[i] = dsu.Port{Slot: i}
	}
	if slot >= 0 {
		p[slot] = dsu.Port{Slot: slot, State: dsu.StateConnected, Model: 2, Connection: 1, MAC: m, Battery: 5}
	}
	return p
}

// waitFor polls cond for up to 2 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); !cond(); {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// delivered sends packets of slot until one arrives.
func (r *rig) delivered(slot int) dsu.Pad {
	r.t.Helper()
	n := len(r.received())
	waitFor(r.t, "a packet", func() bool {
		r.srv.Send(r.pad(slot, mac))
		return len(r.received()) > n
	})
	return r.received()[n]
}

func TestClientRoundTrip(t *testing.T) {
	silent := func(s dsu.State) bool { return !s.Answers && !s.Receiving }
	ready := func(s dsu.State) bool { return s.Answers && !s.Receiving && s.Slot == 0 }
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	r.srv.SetSilent(true)
	// a first answer may have come before the silence: it ages out
	time.Sleep(250 * time.Millisecond)
	if s := r.c.State(); !silent(s) || s.Addr != r.srv.Addr().String() {
		t.Fatalf("a silent server: %+v", s)
	}
	r.srv.SetSilent(false)
	waitFor(t, "the server's answer", func() bool { return ready(r.c.State()) })
	waitFor(t, "the data request", func() bool { return r.srv.Subscribed(0) })
	if _, data := r.srv.Requests(); data == 0 {
		t.Fatal("no data request")
	}
	before := time.Now().Add(time.Hour)
	p := r.delivered(0)
	if p.Slot != 0 || p.MAC != mac || p.Gyro != [3]float32{1.5, -0.25, 0} || p.Accel != [3]float32{0, 1, 0} || p.Micros == 0 {
		t.Errorf("delivered %+v", p)
	}
	r.mu.Lock()
	at := r.at[len(r.at)-1]
	r.mu.Unlock()
	if at.Before(before.Add(-time.Second)) {
		t.Errorf("at %v is not the client's clock", at)
	}
	if s := r.c.State(); !s.Receiving || !s.Answers || s.Slot != 0 {
		t.Errorf("receiving: %+v", s)
	}
	waitFor(t, "no motion for 60 ms", func() bool { return ready(r.c.State()) })
	r.srv.SetSilent(true)
	waitFor(t, "the server silent for 200 ms", func() bool { return silent(r.c.State()) })
	if s := r.c.State(); s.Dropped != 0 {
		t.Errorf("dropped %d", s.Dropped)
	}
}

// TestClientResend: the server forgets a client after 150 ms, and the
// client asks again before that, so the data never stops.
func TestClientResend(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	r.delivered(0)
	var buckets [4]int
	start := time.Now()
	for time.Since(start) < time.Second {
		n := len(r.received())
		r.srv.Send(r.pad(0, mac))
		time.Sleep(5 * time.Millisecond)
		if len(r.received()) > n {
			if i := int(time.Since(start) / (250 * time.Millisecond)); i < len(buckets) {
				buckets[i]++
			}
		}
	}
	for i, n := range buckets {
		if n == 0 {
			t.Errorf("no data in the %d. quarter second: %v", i+1, buckets)
		}
	}
}

// TestClientActive: while the virtual pad is closed only the ports are
// asked; when it opens, the data is asked at once.
func TestClientActive(t *testing.T) {
	r := newRig(t, dsu.Want{Slot: 0, MAC: mac}, func(o *dsu.Options) { o.Resend = time.Second })
	waitFor(t, "the ports", func() bool { return r.c.State().Answers })
	time.Sleep(50 * time.Millisecond)
	if ports, data := r.srv.Requests(); ports == 0 || data != 0 {
		t.Fatalf("inactive: %d port requests, %d data requests", ports, data)
	}
	r.setWant(dsu.Want{Active: true, Slot: 0, MAC: mac})
	waitFor(t, "the first data request", func() bool { _, data := r.srv.Requests(); return data == 1 })
	first := time.Now()
	r.setWant(dsu.Want{Slot: 0, MAC: mac})
	time.Sleep(50 * time.Millisecond)
	r.setWant(dsu.Want{Active: true, Slot: 0, MAC: mac})
	waitFor(t, "the second data request", func() bool { _, data := r.srv.Requests(); return data == 2 })
	if d := time.Since(first); d > 600*time.Millisecond {
		t.Errorf("the data request came %v after the last, as a repeat: not at once", d)
	}
}

// TestClientInactiveData: the server goes on sending for a while after the
// last request; once the virtual pad has closed, none of it is delivered.
func TestClientInactiveData(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, func(o *dsu.Options) { o.Fresh = time.Second })
	r.delivered(0)
	r.setWant(dsu.Want{Slot: 0, MAC: mac})
	time.Sleep(50 * time.Millisecond) // the sender has read it
	n := len(r.received())
	for range 5 {
		r.srv.Send(r.pad(0, mac))
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	if got := len(r.received()); got != n {
		t.Errorf("%d packets delivered while inactive", got-n)
	}
	r.setWant(dsu.Want{Active: true, Slot: 0, MAC: mac})
	r.delivered(0)
}

// TestClientNoData: the server answers with the controller, yet sends no
// motion for it: after Answers of asking, the state says so; motion ends
// it, and so does the virtual pad closing.
func TestClientNoData(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	waitFor(t, "the data request", func() bool { return r.srv.Subscribed(0) })
	if s := r.c.State(); s.NoData {
		t.Fatalf("no data at once: %+v", s)
	}
	waitFor(t, "no data for 200 ms", func() bool { s := r.c.State(); return s.NoData && s.Answers && !s.Receiving })
	r.delivered(0)
	if s := r.c.State(); s.NoData || !s.Receiving {
		t.Errorf("motion came: %+v", s)
	}
	waitFor(t, "no data again", func() bool { return r.c.State().NoData })
	r.setWant(dsu.Want{Slot: 0, MAC: mac})
	waitFor(t, "inactive", func() bool { return !r.c.State().NoData })
	// packets without motion are no data either
	r.setWant(dsu.Want{Active: true, Slot: 0, MAC: mac})
	start := time.Now()
	for time.Since(start) < 400*time.Millisecond {
		r.srv.Send(dsu.Pad{Slot: 0, State: dsu.StateConnected, MAC: mac, Counter: r.counter.Add(1)})
		time.Sleep(5 * time.Millisecond)
	}
	if s := r.c.State(); !s.NoData {
		t.Errorf("packets without motion for 400 ms: %+v", s)
	}
}

// TestClientSlotMoves: the controller moves to slot 2; the client asks for
// slot 2 and drops slot 0's packets.
func TestClientSlotMoves(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	r.delivered(0)
	r.srv.SetPorts(ports(2, mac))
	waitFor(t, "the pick of slot 2", func() bool { return r.c.State().Slot == 2 })
	waitFor(t, "the subscription of slot 2", func() bool { return r.srv.Subscribed(2) })
	n := len(r.received())
	for range 5 {
		r.srv.Send(r.pad(0, mac)) // slot 0's subscription lasts a while yet
	}
	time.Sleep(30 * time.Millisecond)
	if got := r.received(); len(got) != n {
		t.Fatalf("slot 0's packets delivered: %+v", got[n:])
	}
	if p := r.delivered(2); p.Slot != 2 {
		t.Errorf("delivered %+v", p)
	}
}

// TestClientOtherMAC: a known MAC that no slot has picks nothing, so no
// data is asked for.
func TestClientOtherMAC(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: [6]byte{9, 9, 9, 9, 9, 9}}, nil)
	waitFor(t, "the ports", func() bool { return r.c.State().Answers })
	time.Sleep(100 * time.Millisecond)
	if s := r.c.State(); s.Slot != -1 || !s.Answers {
		t.Errorf("state %+v", s)
	}
	if _, data := r.srv.Requests(); data != 0 {
		t.Errorf("%d data requests", data)
	}
}

func TestClientDuplicates(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	r.delivered(0)
	time.Sleep(30 * time.Millisecond) // the packets still on their way
	n := len(r.received())
	p := r.pad(0, mac)
	r.srv.Send(p)
	r.srv.Send(p)
	time.Sleep(50 * time.Millisecond)
	if got := len(r.received()) - n; got != 1 {
		t.Errorf("one packet sent twice, delivered %d times", got)
	}
}

func TestClientMalformed(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	r.delivered(0)
	bad := dsutest.DataReply(0xAABBCCDD, r.pad(0, mac))
	bad[50] ^= 0xFF // the checksum no longer fits
	r.srv.Raw(bad)
	r.srv.Raw([]byte("DSUS"))
	waitFor(t, "two dropped", func() bool { return r.c.State().Dropped == 2 })
	r.delivered(0)
}

// TestClientServerRestarts: a server started again on the same port, with
// a new id, has forgotten the client, which subscribes again.
func TestClientServerRestarts(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	r.delivered(0)
	addr := r.srv.Addr()
	r.srv.Close()
	srv, err := dsutest.Listen(addr)
	if err != nil {
		t.Skipf("the port was taken meanwhile: %v", err)
	}
	defer srv.Close()
	srv.SetID(0x11223344)
	srv.SetForget(150 * time.Millisecond)
	srv.SetPorts(ports(0, mac))
	r.srv = srv
	start := time.Now()
	r.delivered(0)
	t.Logf("data again after %v", time.Since(start))
}

func TestClientRetarget(t *testing.T) {
	r := newRig(t, dsu.Want{Active: true, Slot: 0, MAC: mac}, nil)
	r.delivered(0)
	if changed, err := r.c.Retarget(r.srv.Addr()); changed || err != nil {
		t.Errorf("the same address: %v %v", changed, err)
	}
	second := dsutest.NewServer(t)
	second.SetForget(150 * time.Millisecond)
	second.SetPorts(ports(0, mac))
	if changed, err := r.c.Retarget(second.Addr()); !changed || err != nil {
		t.Fatalf("another server: %v %v", changed, err)
	}
	if s := r.c.State(); s.Addr != second.Addr().String() {
		t.Errorf("right after the move: %+v", s)
	}
	r.srv = second
	r.delivered(0)
	if changed, err := r.c.Retarget(nil); !changed || err != nil {
		t.Fatalf("nil: %v %v", changed, err)
	}
	if s := r.c.State(); s.Addr != "" || s.Answers {
		t.Errorf("asking nowhere: %+v", s)
	}
	ports, _ := second.Requests()
	time.Sleep(100 * time.Millisecond)
	if now, _ := second.Requests(); now != ports {
		t.Errorf("asked %d more times after Retarget(nil)", now-ports)
	}
	if changed, _ := r.c.Retarget(nil); changed {
		t.Error("nil again changed")
	}
}

// TestClientClose: OnPad(nil) stops the packets; Close is safe twice and
// ends every goroutine.
func TestClientClose(t *testing.T) {
	base := runtime.NumGoroutine()
	srv, err := dsutest.Listen(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetPorts(ports(0, mac))
	var n atomic.Int32
	c, err := dsu.New(dsu.Options{Addr: srv.Addr(), Tick: 10 * time.Millisecond, Resend: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	c.OnPad(func(dsu.Pad, time.Time) { n.Add(1) })
	counter := uint32(0)
	send := func() {
		counter++
		srv.Send(dsu.Pad{Slot: 0, State: dsu.StateConnected, MAC: mac, Counter: counter, Micros: uint64(counter)})
	}
	waitFor(t, "a packet", func() bool { send(); return n.Load() > 0 })
	c.OnPad(nil)
	time.Sleep(20 * time.Millisecond)
	stopped := n.Load()
	for range 5 {
		send()
	}
	time.Sleep(30 * time.Millisecond)
	if n.Load() != stopped {
		t.Errorf("%d packets after OnPad(nil)", n.Load()-stopped)
	}
	c.Close()
	c.Close()
	if changed, _ := c.Retarget(srv.Addr()); changed {
		t.Error("Retarget after Close")
	}
	srv.Close()
	waitFor(t, "the goroutines to end", func() bool { return runtime.NumGoroutine() <= base })
}
