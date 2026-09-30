package dsx

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// DS4Windows 5 (the VIIPER builds) listens for DSX's packets too, with
// stricter rules: every value in range, the right number of values for
// each trigger mode, instruction types 0-7 only, at most 64 instructions
// of at most 14 values, controllers 0-7. One instruction it refuses drops
// the whole packet, with no answer. Recognized modes it does not support
// drop the packet too, with an answer that starts "Unsupported". So this
// dialect puts every value in range before the diff, and leaves out an
// instruction that still would not pass, alone.

// DS4WindowsStatus is the status text DS4Windows answers with.
const DS4WindowsStatus = "DS4Windows DSX UDP Server Running"

const (
	ds4wMaxInstructions = 64
	ds4wMaxParameters   = 14
	ds4wMaxControllers  = 8
)

// fromDS4Windows: the status text of an answer from DS4Windows, as it
// answers a packet it took or one it refused as unsupported.
func fromDS4Windows(status string) bool {
	return status == DS4WindowsStatus || strings.HasPrefix(status, "Unsupported DSX")
}

// ds4wState remembers what was logged, so each bad value shows once.
type ds4wState struct {
	mu   sync.Mutex
	seen map[string]bool
}

// once reports whether key is new, and remembers it.
func (s *ds4wState) once(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[key] {
		return false
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[key] = true
	return true
}

// noteRefused: DS4Windows dropped a whole packet it found unsupported, so
// the next frame goes out in full.
func (c *Client) noteRefused(status string) {
	if !strings.HasPrefix(status, "Unsupported") {
		return
	}
	c.refused.Store(true)
	if c.ds4w.once("refused " + status) {
		log.Printf("DS4Windows refused a packet: %s", status)
	}
}

// sendFrameDS4Windows is Send in DS4Windows' dialect.
func (c *Client) sendFrameDS4Windows(controllers []int, prev *Frame, next Frame, out Outputs) {
	if c.refused.Swap(false) {
		prev = nil
	}
	n := c.ds4wFrame(next)
	var p *Frame
	if prev != nil {
		f := c.ds4wFrame(*prev)
		p = &f
	}
	var list []instruction
	for _, i := range controllers {
		list = append(list, changes(i, p, n, out)...)
	}
	c.sendDS4Windows(list)
}

// sendDS4Windows sends the instructions DS4Windows takes, in packets it
// takes.
func (c *Client) sendDS4Windows(list []instruction) {
	ok := list[:0:0]
	for _, in := range list {
		if ds4wAccepts(in) {
			ok = append(ok, in)
		} else if c.ds4w.once(fmt.Sprint("drop ", in.Type, in.Parameters)) {
			log.Printf("DS4Windows: instruction %d %v left out, DS4Windows would refuse it", in.Type, in.Parameters)
		}
	}
	for len(ok) > 0 {
		n := min(len(ok), ds4wMaxInstructions)
		c.send(ok[:n])
		ok = ok[n:]
	}
}

// ds4wFrame puts a frame in DS4Windows' ranges, and logs each value it
// had to change once.
func (c *Client) ds4wFrame(f Frame) Frame {
	out := f
	out.Left, out.Right = c.ds4wTrigger(f.Left), c.ds4wTrigger(f.Right)
	for i, v := range f.Lightbar {
		out.Lightbar[i] = clamp(v, 0, 255)
	}
	out.Brightness = clamp(f.Brightness, 0, 255)
	if out.Lightbar != f.Lightbar || out.Brightness != f.Brightness {
		if c.ds4w.once(fmt.Sprint("light ", f.Lightbar, f.Brightness)) {
			log.Printf("DS4Windows: lightbar %v brightness %d out of range, sent as %v %d", f.Lightbar, f.Brightness, out.Lightbar, out.Brightness)
		}
	}
	out.PlayerLEDs = PlayerLEDs(clamp(int(f.PlayerLEDs), 0, int(LEDsOff)))
	out.Mic = MicLED(clamp(int(f.Mic), int(MicOn), int(MicOff)))
	return out
}

func (c *Client) ds4wTrigger(t Trigger) Trigger {
	s := DS4WindowsTrigger(t)
	if !s.equal(t) && c.ds4w.once(fmt.Sprint("trigger ", t.Mode, t.Params)) {
		log.Printf("DS4Windows: trigger %s %v out of its range, sent as %s %v", t.Mode, t.Params, s.Mode, s.Params)
	}
	return s
}

// DS4WindowsTrigger is t within what DS4Windows takes: each value is
// clamped to its range, and a mode it lacks, or a wrong number of values,
// becomes OFF.
func DS4WindowsTrigger(t Trigger) Trigger {
	p := t.Params
	at := func(i, lo, hi int) int { return clamp(p[i], lo, hi) }
	each := func(from, lo, hi int) []int {
		out := make([]int, len(p))
		for i := range p {
			if i < from {
				out[i] = at(i, 0, 255)
			} else {
				out[i] = at(i, lo, hi)
			}
		}
		return out
	}
	switch {
	case t.Mode == TriggerFeedback && len(p) == 2:
		return Trigger{t.Mode, []int{at(0, 0, 9), at(1, 0, 8)}}
	case t.Mode == TriggerWeapon && len(p) == 3:
		start := at(0, 2, 7)
		return Trigger{t.Mode, []int{start, at(1, start+1, 8), at(2, 0, 8)}}
	case t.Mode == TriggerVibration && len(p) == 3:
		return Trigger{t.Mode, []int{at(0, 0, 9), at(1, 0, 8), at(2, 0, 255)}}
	case t.Mode == TriggerSlopeFeedback && len(p) == 4:
		start := at(0, 0, 8)
		return Trigger{t.Mode, []int{start, at(1, start+1, 9), at(2, 1, 8), at(3, 1, 8)}}
	case t.Mode == TriggerMultiplePositionFeedback && len(p) == 10:
		return Trigger{t.Mode, each(0, 0, 8)}
	case t.Mode == TriggerMultiplePositionVibration && len(p) == 11:
		return Trigger{t.Mode, each(1, 0, 8)} // the frequency first, 0-255
	}
	return TriggerNone()
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

// ds4wAccepts: DS4Windows would take this instruction.
func ds4wAccepts(in instruction) bool {
	p := in.Parameters
	if in.Type == instGetStatus {
		return len(p) == 0
	}
	if len(p) == 0 || len(p) > ds4wMaxParameters || p[0] < 0 || p[0] >= ds4wMaxControllers {
		return false
	}
	switch in.Type {
	case instTriggerUpdate:
		return len(p) >= 3 && (p[1] == triggerLeft || p[1] == triggerRight) && ds4wTriggerAccepted(TriggerMode(p[2]), p[3:])
	case instRGBUpdate:
		return (len(p) == 4 || len(p) == 5) && within(p[1:], 0, 255)
	case instMicLED:
		return len(p) == 2 && within(p[1:], 0, 2)
	case instPlayerLED:
		return len(p) == 2 && within(p[1:], 0, 5)
	case instResetToProfile:
		return len(p) == 1
	}
	return false
}

func ds4wTriggerAccepted(m TriggerMode, p []int) bool {
	if !within(p, 0, 255) {
		return false
	}
	switch m {
	case TriggerOff:
		return len(p) == 0
	case TriggerFeedback:
		return len(p) == 2 && p[0] <= 9 && p[1] <= 8
	case TriggerWeapon:
		return len(p) == 3 && p[0] >= 2 && p[0] <= 7 && p[1] > p[0] && p[1] <= 8 && p[2] <= 8
	case TriggerVibration:
		return len(p) == 3 && p[0] <= 9 && p[1] <= 8
	case TriggerSlopeFeedback:
		return len(p) == 4 && p[0] <= 8 && p[1] > p[0] && p[1] <= 9 && within(p[2:], 1, 8)
	case TriggerMultiplePositionFeedback:
		return len(p) == 10 && within(p, 0, 8)
	case TriggerMultiplePositionVibration:
		return len(p) == 11 && within(p[1:], 0, 8)
	}
	return false
}

func within(p []int, lo, hi int) bool {
	for _, v := range p {
		if v < lo || v > hi {
			return false
		}
	}
	return true
}

// String is the mode's name as the settings file has it.
func (m TriggerMode) String() string {
	for name, v := range triggerModes {
		if v == m {
			return name
		}
	}
	return fmt.Sprintf("mode %d", int(m))
}

// Probe asks the listener at addr for its status: DS4Windows answers with
// its own status text, DSX with any other. ok is false when nothing
// answers within timeout.
func Probe(addr *net.UDPAddr, timeout time.Duration) (d Dialect, ok bool) {
	conn, err := net.DialUDP(network(addr), nil, addr)
	if err != nil {
		return DSX, false
	}
	defer conn.Close()
	b, _ := json.Marshal(packet{Instructions: []instruction{{Type: instGetStatus, Parameters: []int{}}}})
	if _, err := conn.Write(b); err != nil {
		return DSX, false
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			// a timeout, or nobody on the port (Windows reports ICMP
			// "port unreachable" as a read error)
			return DSX, false
		}
		var r response
		switch {
		case json.Unmarshal(buf[:n], &r) != nil:
		case r.Status == DS4WindowsStatus:
			return DS4Windows, true
		case fromDS4Windows(r.Status):
			// an answer to some other packet
		default:
			return DSX, true
		}
	}
}
