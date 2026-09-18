// Package dsx drives controllers through DSX's Mod System: UDP packets of
// JSON instructions. Verified against Paliverse's "Mod System (DSX v3)"
// example and the enums in DSX.dll 3.2.0.
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
	triggerLeft         = 1
	triggerRight        = 2
	defaultPort         = 6969
	onlineAfterResponse = 6 * time.Second
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
	DeviceType        any
	ConnectionType    any
	BatteryLevel      int
	IsSupportAT       bool
	IsSupportLightBar bool
}

type response struct {
	Devices []device
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
	conn    *net.UDPConn
	verbose bool

	mu           sync.Mutex
	devices      []device
	lastResponse time.Time
}

func NewClient(port int, verbose bool) (*Client, error) {
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, verbose: verbose}
	go c.readResponses()
	return c, nil
}

func (c *Client) Close() { _ = c.conn.Close() }

// DSX answers every packet with its status and controller list.
func (c *Client) readResponses() {
	buf := make([]byte, 65536)
	for {
		n, err := c.conn.Read(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			// DSX not running: Windows reports ICMP "port unreachable" as a read error
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var r response
		if json.Unmarshal(buf[:n], &r) != nil {
			continue
		}
		c.mu.Lock()
		c.lastResponse = time.Now()
		if r.Devices != nil {
			if !slices.Equal(indices(c.devices), indices(r.Devices)) {
				log.Printf("DSX: %d controller(s): %s", len(r.Devices), describe(r.Devices))
			}
			c.devices = r.Devices
		}
		c.mu.Unlock()
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
	_, _ = c.conn.Write(b)
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
	c.send(list)
}

// Send sends what changed from prev to next (with no prev, everything).
func (c *Client) Send(controllers []int, prev *Frame, next Frame, out Outputs) {
	var list []instruction
	for _, i := range controllers {
		list = append(list, changes(i, prev, next, out)...)
	}
	c.send(list)
}
