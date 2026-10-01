// Package dsx drives controllers through DSX's Mod System: UDP packets of
// JSON instructions. Verified against Paliverse's "Mod System (DSX v3)"
// example and the enums in DSX.dll 3.2.0. DS4Windows 5 listens for the
// same packets, with stricter rules (its dialect, ds4windows.go).
package dsx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DSX.ModificationSystem.InstructionType
const (
	instGetStatus       = 0
	instTriggerUpdate   = 1
	instRGBUpdate       = 2
	instMicLED          = 5
	instPlayerLED       = 6 // PlayerLEDNewRevision
	instResetToProfile  = 7 // ResetToUserSettings
	instToMode          = 8 // [controller, category, mode]; not in the public docs
	triggerLeft         = 1
	triggerRight        = 2
	defaultPort         = 6969
	onlineAfterResponse = 6 * time.Second
)

// ToMode, read from DSX.dll 3.2.0: category 0/1 sticks, 2 motion, 3
// touchpad; the mode is that page's enum. DSX keeps one ToMode per
// controller and ResetToUserSettings does not clear it, so handing a page
// back to the profile is a ToMode that matches no category.
const toModeMotion = 2

// MotionMode is what DSX's motion page does (DSX.Enums.MotionMode), set per
// controller with ToMode [controller, 2, mode].
type MotionMode int

const (
	MotionProfile  MotionMode = -1 // hand back: [c, -1, -1] matches no category, so the profile's mode applies
	MotionNone     MotionMode = 0  // no mouse; the motion still reaches the virtual DualSense
	MotionToMouse  MotionMode = 1  // DSX's own motion to mouse, with the profile's settings
	MotionDisabled MotionMode = 7  // no mouse, and the virtual DualSense's motion is zeroed
)

type instruction struct {
	Type       int   `json:"type"`
	Parameters []int `json:"parameters"`
}

type packet struct {
	Instructions []instruction `json:"instructions"`
}

// device as DSX reports it (enums arrive as numbers or strings).
type device struct {
	Index             int
	MacAddress        any // a string from DS4Windows; taken as any, so no other type spoils the reply
	DeviceType        any
	ConnectionType    any
	BatteryLevel      int
	IsSupportAT       bool
	IsSupportLightBar bool
}

type response struct {
	Status  string
	Devices []device
}

// Dialect is which program the client speaks to.
type Dialect int

const (
	DSX        Dialect = iota
	DS4Windows         // DS4Windows' DSX listener
)

func (d Dialect) String() string {
	if d == DS4Windows {
		return "DS4Windows"
	}
	return "DSX"
}

// Port reads DSX's port file (%LOCALAPPDATA%\DSX\DSX_UDP_PortNumber.txt)
// unless one is set, falling back to 6969.
func Port(configured int) int {
	if configured > 0 {
		return configured
	}
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return defaultPort
	}
	b, err := os.ReadFile(filepath.Join(local, "DSX", "DSX_UDP_PortNumber.txt"))
	if err == nil {
		if p, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return defaultPort
}

// Client sends instructions to DSX and tracks the controllers it reports.
type Client struct {
	conn    atomic.Pointer[net.UDPConn] // Retarget swaps it
	closed  atomic.Bool
	verbose bool
	dialect Dialect
	ds4w    ds4wState
	refused atomic.Bool // DS4Windows refused a packet: send everything again

	mu           sync.Mutex
	devices      []device
	lastResponse time.Time
	foreign      bool // logged that another program answers
}

// NewClient speaks to DSX on this PC.
func NewClient(port int, verbose bool) (*Client, error) {
	return NewClientTo(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}, verbose, DSX)
}

// NewClientTo speaks dialect to the listener at addr.
func NewClientTo(addr *net.UDPAddr, verbose bool, dialect Dialect) (*Client, error) {
	conn, err := net.DialUDP(network(addr), nil, addr)
	if err != nil {
		return nil, err
	}
	c := &Client{verbose: verbose, dialect: dialect}
	c.conn.Store(conn)
	go c.readResponses(conn)
	return c, nil
}

// network: udp6 for an IPv6 address such as ::1.
func network(addr *net.UDPAddr) string {
	if addr.IP != nil && addr.IP.To4() == nil {
		return "udp6"
	}
	return "udp4"
}

func (c *Client) Close() {
	c.closed.Store(true)
	_ = c.conn.Load().Close()
}

// Addr is where the client sends, as Retarget last moved it.
func (c *Client) Addr() string { return c.conn.Load().RemoteAddr().String() }

// Retarget speaks to the listener at addr from now on, and reports
// whether that is a change. It is for DS4Windows, whose listener may move
// when another DS4Windows starts.
func (c *Client) Retarget(addr *net.UDPAddr) (bool, error) {
	if c.closed.Load() || c.conn.Load().RemoteAddr().String() == addr.String() {
		return false, nil
	}
	conn, err := net.DialUDP(network(addr), nil, addr)
	if err != nil {
		return false, err
	}
	old := c.conn.Swap(conn)
	go c.readResponses(conn)
	_ = old.Close()
	if c.closed.Load() { // closed meanwhile
		_ = conn.Close()
	}
	c.mu.Lock()
	c.foreign = false
	c.mu.Unlock()
	return true, nil
}

// DSX answers every packet with its status and controller list.
func (c *Client) readResponses(conn *net.UDPConn) {
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			// DSX not running: Windows reports ICMP "port unreachable" as a read error
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var r response
		if json.Unmarshal(buf[:n], &r) != nil || !c.answers(r.Status) {
			continue
		}
		c.mu.Lock()
		c.lastResponse = time.Now()
		if r.Devices != nil {
			if !slices.Equal(indices(c.devices), indices(r.Devices)) {
				log.Printf("%s: %d controller(s): %s", c.dialect, len(r.Devices), describe(r.Devices))
			}
			c.devices = r.Devices
		}
		c.mu.Unlock()
	}
}

// answers: the reply comes from the program this client speaks to. DSX
// never gets DS4Windows' answers counted, so a DS4Windows on DSX's port is
// not taken for DSX.
func (c *Client) answers(status string) bool {
	if c.dialect == DSX {
		if !fromDS4Windows(status) {
			return true
		}
		c.onceForeign("DSX: DS4Windows answers on %s, where EDSense looks for DSX, so it waits for DSX. To use DS4Windows, set \"backend\" to \"ds4windows\" (or \"auto\") in EDSense's settings and start EDSense again")
		return false
	}
	if !fromDS4Windows(status) {
		c.onceForeign("DS4Windows: another program answers on %s (DSX?), so EDSense waits for DS4Windows")
		return false
	}
	c.noteRefused(status)
	return true
}

// onceForeign logs, once, that another program answers than the one this
// client speaks to; format takes the address.
func (c *Client) onceForeign(format string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.foreign {
		log.Printf(format, c.conn.Load().RemoteAddr())
		c.foreign = true
	}
}

func indices(ds []device) []int {
	out := make([]int, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Index)
	}
	return out
}

func describe(ds []device) string {
	if len(ds) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(ds))
	for _, d := range ds {
		parts = append(parts, fmt.Sprintf("#%d %v/%v battery %d%% adaptive triggers %v", d.Index, d.DeviceType, d.ConnectionType, d.BatteryLevel, d.IsSupportAT))
	}
	return strings.Join(parts, ", ")
}

// Online reports whether DSX answered lately.
func (c *Client) Online() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.lastResponse.IsZero() && time.Since(c.lastResponse) < onlineAfterResponse
}

// MAC is the MAC address of the controller at index, as the program
// reports it, or "".
func (c *Client) MAC(index int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.devices {
		if d.Index == index {
			s, _ := d.MacAddress.(string)
			return s
		}
	}
	return ""
}

// Controllers are the indices of the controllers to drive. Before DSX has
// answered at all, the first one is tried.
func (c *Client) Controllers() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []int{}
	for _, d := range c.devices {
		if d.IsSupportAT || d.IsSupportLightBar {
			out = append(out, d.Index)
		}
	}
	if len(out) == 0 && c.lastResponse.IsZero() {
		out = append(out, 0)
	}
	return out
}

func (c *Client) send(list []instruction) {
	if len(list) == 0 {
		return
	}
	b, err := json.Marshal(packet{Instructions: list})
	if err != nil {
		return
	}
	if c.verbose {
		log.Printf("-> %s", b)
	}
	_, _ = c.conn.Load().Write(b)
}

// RequestStatus asks DSX for its controller list.
func (c *Client) RequestStatus() {
	c.send([]instruction{{Type: instGetStatus, Parameters: []int{}}})
}

// ResetToProfile hands the controllers back to the user's DSX profile.
func (c *Client) ResetToProfile(controllers []int) {
	var list []instruction
	for _, i := range controllers {
		list = append(list, instruction{Type: instResetToProfile, Parameters: []int{i}})
	}
	if c.dialect == DS4Windows {
		c.sendDS4Windows(list)
		return
	}
	c.send(list)
}

// SetMotion sets what DSX's motion page does, or hands it back to the
// profile. DSX drops mod instructions after a minute without UDP traffic,
// so an override lapses if EDSense dies. DS4Windows has no such
// instruction and drops a packet with one, so nothing is sent to it.
func (c *Client) SetMotion(controllers []int, m MotionMode) {
	if c.dialect == DS4Windows {
		return
	}
	c.send(motionInstructions(controllers, m))
}

func motionInstructions(controllers []int, m MotionMode) []instruction {
	category, mode := toModeMotion, int(m)
	if m == MotionProfile {
		category = int(MotionProfile)
	}
	var list []instruction
	for _, i := range controllers {
		list = append(list, instruction{Type: instToMode, Parameters: []int{i, category, mode}})
	}
	return list
}

// Send sends what changed from prev to next (with no prev, everything).
func (c *Client) Send(controllers []int, prev *Frame, next Frame, out Outputs) {
	if c.dialect == DS4Windows {
		c.sendFrameDS4Windows(controllers, prev, next, out)
		return
	}
	var list []instruction
	for _, i := range controllers {
		list = append(list, changes(i, prev, next, out)...)
	}
	c.send(list)
}
