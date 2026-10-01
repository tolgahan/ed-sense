package dsx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// ds4wParse checks a packet the way DS4Windows' DSX listener does
// (DS4Windows RC4.6.6, DSXUdpServer.TryParsePacket): the whole packet is
// refused when one instruction is. It is written from those rules on its
// own, apart from the dialect's ds4wAccepts, so the two check each other.
// A recognized mode DS4Windows does not support is an error too.
func ds4wParse(b []byte) error {
	if len(b) == 0 || len(b) > 16384 {
		return fmt.Errorf("packet of %d bytes", len(b))
	}
	var root map[string]json.RawMessage
	if err := strictJSON(b, &root); err != nil {
		return err
	}
	raw, ok := root["instructions"]
	if len(root) != 1 || !ok {
		return errors.New("the packet needs exactly the instructions property")
	}
	var list []map[string]json.RawMessage
	if err := strictJSON(raw, &list); err != nil {
		return err
	}
	if len(list) == 0 || len(list) > 64 {
		return fmt.Errorf("%d instructions", len(list))
	}
	for _, in := range list {
		if err := ds4wParseInstruction(in); err != nil {
			return fmt.Errorf("%s: %w", mustJSON(in), err)
		}
	}
	return nil
}

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func ds4wParseInstruction(in map[string]json.RawMessage) error {
	rawType, ok1 := in["type"]
	rawParams, ok2 := in["parameters"]
	if len(in) != 2 || !ok1 || !ok2 {
		return errors.New("an instruction needs exactly type and parameters")
	}
	var typ json.Number
	if err := strictJSON(rawType, &typ); err != nil {
		return err
	}
	t, err := typ.Int64()
	if err != nil || t < 0 || t > 7 {
		return fmt.Errorf("type %s", typ)
	}
	if string(rawParams) == "null" && t == 0 {
		return nil
	}
	var nums []json.Number
	if err := strictJSON(rawParams, &nums); err != nil {
		return err
	}
	if len(nums) > 14 {
		return fmt.Errorf("%d parameters", len(nums))
	}
	p := make([]int, len(nums))
	for i, n := range nums {
		v, err := n.Int64()
		if err != nil || v < -1<<31 || v >= 1<<31 {
			return fmt.Errorf("parameter %s", n)
		}
		p[i] = int(v)
	}
	if t == 0 {
		if len(p) != 0 {
			return errors.New("GetDSXStatus takes no parameters")
		}
		return nil
	}
	if len(p) == 0 || p[0] < 0 || p[0] >= 8 {
		return errors.New("controller index")
	}
	between := func(v []int, lo, hi int) bool {
		for _, x := range v {
			if x < lo || x > hi {
				return false
			}
		}
		return true
	}
	switch t {
	case 1:
		if len(p) < 3 || p[1] != 1 && p[1] != 2 {
			return errors.New("trigger side")
		}
		return ds4wParseTrigger(p[2], p[3:])
	case 2:
		if len(p) != 4 && len(p) != 5 || !between(p[1:], 0, 255) {
			return errors.New("RGB")
		}
	case 3:
		if len(p) != 6 || !between(p[1:], 0, 1) {
			return errors.New("legacy player LEDs")
		}
	case 4:
		if len(p) != 3 || p[1] != 1 && p[1] != 2 || !between(p[2:], 0, 255) {
			return errors.New("trigger threshold")
		}
		if p[2] != 0 {
			return errors.New("unsupported: nonzero trigger threshold")
		}
	case 5:
		if len(p) != 2 || !between(p[1:], 0, 2) {
			return errors.New("mic LED")
		}
	case 6:
		if len(p) != 2 || !between(p[1:], 0, 5) {
			return errors.New("player LEDs")
		}
	case 7:
		if len(p) != 1 {
			return errors.New("ResetToUserSettings")
		}
	}
	return nil
}

func ds4wParseTrigger(mode int, p []int) error {
	for _, v := range p {
		if v < 0 || v > 255 {
			return fmt.Errorf("trigger value %d", v)
		}
	}
	bad := func(ok bool) error {
		if ok {
			return nil
		}
		return fmt.Errorf("trigger mode %d %v", mode, p)
	}
	upTo := func(v []int, hi int) bool {
		for _, x := range v {
			if x > hi {
				return false
			}
		}
		return true
	}
	switch mode {
	case 2, 3, 4, 5, 6, 7, 9, 10, 11, 19:
		if len(p) == 0 {
			return errors.New("unsupported legacy trigger mode")
		}
		return bad(false)
	case 8:
		if len(p) == 1 {
			return errors.New("unsupported legacy trigger mode")
		}
		return bad(false)
	case 0, 20, 1:
		return bad(len(p) == 0)
	case 12:
		if len(p) == 8 && p[0] >= 9 && p[0] <= 16 {
			return errors.New("unsupported custom trigger submode")
		}
		return bad(len(p) == 8 && p[0] <= 8)
	case 13, 21:
		return bad(len(p) == 2 && p[0] <= 9 && p[1] <= 8)
	case 16, 22:
		return bad(len(p) == 3 && p[0] >= 2 && p[0] <= 7 && p[1] > p[0] && p[1] <= 8 && p[2] <= 8)
	case 17, 23:
		return bad(len(p) == 3 && p[0] <= 9 && p[1] <= 8)
	case 14:
		return bad(len(p) == 4 && p[0] <= 7 && p[1] > p[0] && p[1] <= 8 && p[2] <= 8 && p[3] <= 8)
	case 15:
		return bad(len(p) == 5 && p[0] <= 8 && p[1] > p[0] && p[1] <= 9 && p[2] <= 6 && p[3] > p[2] && p[3] <= 7)
	case 18:
		return bad(len(p) == 6 && p[0] <= 8 && p[1] > p[0] && p[1] <= 9 && p[2] <= 7 && p[3] <= 7 && p[5] <= 2)
	case 24:
		return bad(len(p) == 4 && p[0] <= 8 && p[1] > p[0] && p[1] <= 9 && p[2] >= 1 && p[2] <= 8 && p[3] >= 1 && p[3] <= 8)
	case 25:
		return bad(len(p) == 10 && upTo(p, 8))
	case 26:
		return bad(len(p) == 11 && upTo(p[1:], 8))
	}
	return bad(false)
}

// sink is a local port that keeps every packet sent to it.
type sink struct {
	conn *net.UDPConn
	got  chan []byte
}

func newSink(t *testing.T) *sink {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	s := &sink{conn: conn, got: make(chan []byte, 4096)}
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			s.got <- slices.Clone(buf[:n])
		}
	}()
	t.Cleanup(func() { conn.Close() })
	return s
}

func (s *sink) addr() *net.UDPAddr { return s.conn.LocalAddr().(*net.UDPAddr) }

// drain returns what arrived until nothing came for a moment.
func (s *sink) drain() [][]byte {
	var out [][]byte
	for {
		select {
		case b := <-s.got:
			out = append(out, b)
		case <-time.After(100 * time.Millisecond):
			return out
		}
	}
}

func ds4wClient(t *testing.T, to *net.UDPAddr) *Client {
	c, err := NewClientTo(to, false, DS4Windows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

var allOutputs = Outputs{Triggers: true, Lightbar: true, PlayerLEDs: true, Mic: true}

// oddTriggers: every mode by name, with values in and out of range and
// the wrong number of them, and a mode DSX lacks.
func oddTriggers(r *rand.Rand) []Trigger {
	var out []Trigger
	for name := range triggerModes {
		for n := 0; n <= 12; n++ {
			for range 6 {
				p := make([]int, n)
				for i := range p {
					p[i] = r.Intn(40) - 10
					if r.Intn(8) == 0 {
						p[i] = r.Intn(700) - 200
					}
				}
				out = append(out, NewTrigger(name, p))
			}
		}
	}
	return append(out, Trigger{Mode: 2}, Trigger{Mode: 12, Params: []int{9, 1, 1, 1, 1, 1, 1, 1}}, Trigger{Mode: 99, Params: []int{1}})
}

// TestDS4WindowsAccepts: every packet the dialect sends, for any trigger
// a settings file can hold, any light values, and controllers DS4Windows
// does not have, passes DS4Windows' rules, and none has a ToMode (type 8).
func TestDS4WindowsAccepts(t *testing.T) {
	s := newSink(t)
	c := ds4wClient(t, s.addr())
	r := rand.New(rand.NewSource(1))
	triggers := oddTriggers(r)
	var prev *Frame
	for i, tr := range triggers {
		f := DarkFrame()
		f.Right = tr
		f.Left = triggers[(i*7)%len(triggers)]
		f.Lightbar = [3]int{r.Intn(600) - 150, r.Intn(300), r.Intn(256)}
		f.Brightness = r.Intn(600) - 150
		f.PlayerLEDs = PlayerLEDs(r.Intn(10) - 2)
		f.Mic = MicLED(r.Intn(6) - 2)
		controllers := []int{0}
		if i%5 == 0 {
			controllers = []int{0, 1, 7, 8, -1}
		}
		c.Send(controllers, prev, f, allOutputs)
		if i%3 == 0 {
			prev = nil
		} else {
			prev = &f
		}
	}
	c.RequestStatus()
	c.ResetToProfile([]int{0, 3, 8, -2})
	c.SetMotion([]int{0}, MotionNone)
	c.SetMotion([]int{0}, MotionProfile)
	packets := s.drain()
	if len(packets) < len(triggers)/2 {
		t.Fatalf("only %d packets arrived", len(packets))
	}
	for _, p := range packets {
		if err := ds4wParse(p); err != nil {
			t.Fatalf("DS4Windows would refuse %s: %v", p, err)
		}
		if strings.Contains(string(p), `"type":8`) {
			t.Fatalf("ToMode sent to DS4Windows: %s", p)
		}
	}
}

// TestDS4WindowsTrigger: values DS4Windows takes pass unchanged, and the
// rest is brought into range.
func TestDS4WindowsTrigger(t *testing.T) {
	for _, c := range []struct {
		in, want Trigger
	}{
		{NewTrigger("WEAPON", []int{2, 5, 6}), NewTrigger("WEAPON", []int{2, 5, 6})},
		{NewTrigger("FEEDBACK", []int{0, 0}), NewTrigger("FEEDBACK", []int{0, 0})},
		{NewTrigger("VIBRATION", []int{2, 6, 12}), NewTrigger("VIBRATION", []int{2, 6, 12})},
		{NewTrigger("WEAPON", []int{1, 9, 9}), NewTrigger("WEAPON", []int{2, 8, 8})},
		{NewTrigger("WEAPON", []int{7, 3, 4}), NewTrigger("WEAPON", []int{7, 8, 4})},
		{NewTrigger("FEEDBACK", []int{12, -3}), NewTrigger("FEEDBACK", []int{9, 0})},
		{NewTrigger("VIBRATION", []int{2, 6, 300}), NewTrigger("VIBRATION", []int{2, 6, 255})},
		{NewTrigger("SLOPE_FEEDBACK", []int{9, 2, 0, 12}), NewTrigger("SLOPE_FEEDBACK", []int{8, 9, 1, 8})},
		{NewTrigger("MULTIPLE_POSITION_VIBRATION", []int{300, 9, 0, 1, 2, 3, 4, 5, 6, 7, -1}), NewTrigger("MULTIPLE_POSITION_VIBRATION", []int{255, 8, 0, 1, 2, 3, 4, 5, 6, 7, 0})},
		{NewTrigger("FEEDBACK", []int{2}), TriggerNone()},
		{NewTrigger("OFF", []int{1, 2}), TriggerNone()},
		{Trigger{Mode: 2}, TriggerNone()},
	} {
		if got := DS4WindowsTrigger(c.in); !got.equal(c.want) {
			t.Errorf("%s %v: %s %v, want %s %v", c.in.Mode, c.in.Params, got.Mode, got.Params, c.want.Mode, c.want.Params)
		}
	}
}

// FuzzDS4WSanitize: whatever the settings say, the sanitized trigger is
// one DS4Windows takes, sanitizing again changes nothing, and a trigger it
// already took is left as it was.
func FuzzDS4WSanitize(f *testing.F) {
	f.Add(22, []byte{2, 5, 6})
	f.Add(21, []byte{0, 9})
	f.Add(23, []byte{9, 8, 255})
	f.Add(24, []byte{0, 9, 1, 8})
	f.Add(25, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 8})
	f.Add(26, []byte{40, 0, 1, 2, 3, 4, 5, 6, 7, 8, 8})
	f.Add(22, []byte{200, 1, 250})
	f.Add(3, []byte{})
	f.Fuzz(func(t *testing.T, mode int, raw []byte) {
		p := make([]int, len(raw))
		for i, b := range raw {
			p[i] = int(int8(b)) * 3 // negative and past 255 too
		}
		in := Trigger{Mode: TriggerMode(mode), Params: p}
		out := DS4WindowsTrigger(in)
		list := changes(0, nil, Frame{Left: out, Right: out}, Outputs{Triggers: true})
		b, _ := json.Marshal(packet{Instructions: list})
		if err := ds4wParse(b); err != nil {
			t.Fatalf("%v %v sanitized to %v %v, refused: %v", in.Mode, in.Params, out.Mode, out.Params, err)
		}
		for _, one := range list {
			if !ds4wAccepts(one) {
				t.Fatalf("ds4wAccepts refuses %v, which DS4Windows takes", one)
			}
		}
		if again := DS4WindowsTrigger(out); !again.equal(out) {
			t.Fatalf("sanitizing twice: %v then %v", out.Params, again.Params)
		}
		if ds4wTriggerAccepted(in.Mode, in.Params) && !out.equal(in) {
			t.Fatalf("%v %v was fine, changed to %v %v", in.Mode, in.Params, out.Mode, out.Params)
		}
		whole := instruction{Type: instTriggerUpdate, Parameters: append([]int{0, triggerRight, int(in.Mode)}, in.Params...)}
		one, _ := json.Marshal(packet{Instructions: []instruction{whole}})
		if accepted, parsed := ds4wAccepts(whole), ds4wParse(one) == nil; accepted && !parsed {
			t.Fatalf("ds4wAccepts takes %s, DS4Windows would refuse it", one)
		}
	})
}

// answering is a sink that answers every packet with status, as
// DS4Windows or DSX would.
func answering(t *testing.T, status string) *sink {
	return answeringWith(t, status, "A1:B2:C3:D4:E5:F6")
}

// answeringWith: the controller's MAC address as given, of any type.
func answeringWith(t *testing.T, status string, mac any) *sink {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	s := &sink{conn: conn, got: make(chan []byte, 4096)}
	reply, _ := json.Marshal(map[string]any{"Status": status, "Devices": []map[string]any{{"Index": 1, "MacAddress": mac, "DeviceType": 0, "ConnectionType": 0, "BatteryLevel": 60, "IsSupportAT": true, "IsSupportLightBar": true}}})
	go func() {
		buf := make([]byte, 65536)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			s.got <- slices.Clone(buf[:n])
			_, _ = conn.WriteToUDP(reply, from)
		}
	}()
	return s
}

func waitOnline(c *Client) bool {
	c.RequestStatus()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if c.Online() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestDialectAnswers: each dialect counts only its own program's answers,
// so DS4Windows on DSX's port never counts as DSX, and the other way.
func TestDialectAnswers(t *testing.T) {
	ds4w := answering(t, DS4WindowsStatus)
	dsx := answering(t, "DSX Received UDP Instructions")
	for _, c := range []struct {
		name    string
		to      *sink
		dialect Dialect
		online  bool
	}{
		{"DS4Windows to DS4Windows", ds4w, DS4Windows, true},
		{"DS4Windows to DSX", dsx, DS4Windows, false},
		{"DSX to DS4Windows", ds4w, DSX, false},
		{"DSX to DSX", dsx, DSX, true},
	} {
		cl, err := NewClientTo(c.to.addr(), false, c.dialect)
		if err != nil {
			t.Fatal(err)
		}
		if got := waitOnline(cl); got != c.online {
			t.Errorf("%s: online %v", c.name, got)
		}
		if c.online && !slices.Equal(cl.Controllers(), []int{1}) {
			t.Errorf("%s: controllers %v", c.name, cl.Controllers())
		}
		cl.Close()
	}
}

// TestDS4WindowsRefused: after an "Unsupported" answer the next frame goes
// out in full, since DS4Windows dropped the whole packet.
func TestDS4WindowsRefused(t *testing.T) {
	s := answering(t, "Unsupported DSX legacy trigger mode: 2")
	c := ds4wClient(t, s.addr())
	if !waitOnline(c) {
		t.Fatal("an Unsupported answer is DS4Windows answering")
	}
	s.drain()
	f := weaponsFrame()
	c.Send([]int{1}, &f, f, allOutputs)
	got := s.drain()
	if len(got) != 1 || !strings.Contains(string(got[0]), `"type":2,`) {
		t.Fatalf("after a refusal the frame was not sent in full: %q", got)
	}
}

// TestDS4WindowsNoMotion: SetMotion sends nothing to DS4Windows.
func TestDS4WindowsNoMotion(t *testing.T) {
	s := newSink(t)
	c := ds4wClient(t, s.addr())
	c.SetMotion([]int{0}, MotionNone)
	c.SetMotion([]int{0}, MotionProfile)
	if got := s.drain(); len(got) != 0 {
		t.Fatalf("sent %q", got)
	}
}

// TestClientMAC: the controller's MAC address, for DS4Windows' linked
// profiles; a reply with another type there still counts.
func TestClientMAC(t *testing.T) {
	c := ds4wClient(t, answering(t, DS4WindowsStatus).addr())
	if !waitOnline(c) {
		t.Fatal("DS4Windows did not answer")
	}
	if got := c.MAC(1); got != "A1:B2:C3:D4:E5:F6" {
		t.Errorf("MAC %q", got)
	}
	if got := c.MAC(0); got != "" {
		t.Errorf("MAC of a controller not reported: %q", got)
	}
	n := ds4wClient(t, answeringWith(t, DS4WindowsStatus, 12).addr())
	if !waitOnline(n) || n.MAC(1) != "" || !slices.Equal(n.Controllers(), []int{1}) {
		t.Errorf("a number as MAC address: online %v, MAC %q, controllers %v", n.Online(), n.MAC(1), n.Controllers())
	}
}

// TestRetarget: the client can move to another listener; what it sends
// goes there, and its answers count.
func TestRetarget(t *testing.T) {
	quiet, ds4w := newSink(t), answering(t, DS4WindowsStatus)
	c := ds4wClient(t, quiet.addr())
	if changed, err := c.Retarget(quiet.addr()); changed || err != nil {
		t.Fatalf("the same listener: %v %v", changed, err)
	}
	if changed, err := c.Retarget(ds4w.addr()); !changed || err != nil {
		t.Fatalf("another listener: %v %v", changed, err)
	}
	quiet.drain()
	ds4w.drain()
	if !waitOnline(c) {
		t.Fatal("the new listener's answers do not count")
	}
	if got := quiet.drain(); len(got) != 0 {
		t.Errorf("the old listener still gets %q", got)
	}
	c.Close()
	if changed, _ := c.Retarget(quiet.addr()); changed {
		t.Error("a closed client moved")
	}
}

// TestClientAddr: Addr is where the client sends, and follows Retarget.
func TestClientAddr(t *testing.T) {
	first, second := newSink(t), newSink(t)
	d, err := NewClient(first.addr().Port, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got, want := d.Addr(), fmt.Sprintf("127.0.0.1:%d", first.addr().Port); got != want {
		t.Errorf("DSX: %s, want %s", got, want)
	}
	c := ds4wClient(t, first.addr())
	if got := c.Addr(); got != first.addr().String() {
		t.Errorf("DS4Windows: %s, want %s", got, first.addr())
	}
	if _, err := c.Retarget(second.addr()); err != nil {
		t.Fatal(err)
	}
	if got := c.Addr(); got != second.addr().String() {
		t.Errorf("after Retarget: %s, want %s", got, second.addr())
	}
	c.Close()
	if got := c.Addr(); got != second.addr().String() {
		t.Errorf("closed: %s, want %s", got, second.addr())
	}
}

// TestDSXIgnoresDS4Windows: the DSX dialect tells once why DS4Windows'
// answers do not count.
func TestDSXIgnoresDS4Windows(t *testing.T) {
	var buf lockedBuffer
	old, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(old)
		log.SetFlags(flags)
	}()
	c, err := NewClientTo(answering(t, DS4WindowsStatus).addr(), false, DSX)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitOnline(c)
	waitOnline(c)
	c.Close()
	if n := strings.Count(buf.String(), "DS4Windows answers on"); n != 1 {
		t.Errorf("told %d times: %s", n, buf.String())
	}
}

// lockedBuffer is a log's output that a test reads while the client's
// goroutine may write.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestProbe(t *testing.T) {
	ds4w := answering(t, DS4WindowsStatus)
	dsx := answering(t, "DSX Received UDP Instructions")
	quiet := newSink(t)
	for _, c := range []struct {
		name string
		to   *net.UDPAddr
		want Dialect
		ok   bool
	}{
		{"DS4Windows", ds4w.addr(), DS4Windows, true},
		{"DSX", dsx.addr(), DSX, true},
		{"nobody answers", quiet.addr(), DSX, false},
	} {
		d, ok := Probe(c.to, 300*time.Millisecond)
		if d != c.want || ok != c.ok {
			t.Errorf("%s: %v %v", c.name, d, ok)
		}
	}
	if got := <-ds4w.got; string(got) != `{"instructions":[{"type":0,"parameters":[]}]}` {
		t.Errorf("probe packet %s", got)
	}
}
