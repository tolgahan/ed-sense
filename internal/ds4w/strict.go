package ds4w

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
)

// The strict reader: what EDSense checks before and after it writes one of
// DS4Windows' files. DS4Windows reads them with .NET's XmlSerializer,
// which throws on things EDSense's own readers let pass: an enum or a bool
// attribute in another case, a second root, a duplicate attribute. A
// file it cannot read loads as empty (Auto Profiles.xml) or as defaults
// (Profiles.xml), and its next save drops what the player had. So these
// checks are exact, and never use the lenient readers in files.go.

// errUTF16: EDSense edits files byte for byte, and only in UTF-8.
var errUTF16 = errors.New("it is saved as UTF-16")

// node is an element as the strict reader found it, with where its parts
// are in the file's bytes.
type node struct {
	name xml.Name
	attr []xml.Attr
	// the start tag is [start, open), the end tag [close, end); an empty
	// element written as <a/> has open == close == end
	start, open, close, end int
	empty                   bool   // written as <a/>
	text                    string // its own character data
	kids                    []*node
}

// first is the first child called local.
func (n *node) first(local string) *node {
	for _, k := range n.kids {
		if k.name.Space == "" && k.name.Local == local {
			return k
		}
	}
	return nil
}

// attrValue is the value of the attribute called local.
func (n *node) attrValue(local string) (string, bool) {
	for _, a := range n.attr {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// doc is a file the strict reader read.
type doc struct {
	b    []byte
	bom  int // the length of a UTF-8 byte order mark at the start
	root *node
}

// eol is the file's line end: CRLF unless the first line ends in LF
// alone.
func (d *doc) eol() string {
	if i := bytes.IndexByte(d.b, '\n'); i > 0 && d.b[i-1] != '\r' {
		return "\n"
	}
	return "\r\n"
}

// isUTF16: the bytes look like UTF-16, with or without a byte order mark.
func isUTF16(b []byte) bool {
	if len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF) {
		return true
	}
	head := b
	if len(head) > 64 {
		head = head[:64]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// xmlSpace: XML's white space.
func xmlSpace(s string) bool {
	return strings.Trim(s, " \t\r\n") == ""
}

// scan reads b strictly: one root element, nothing but comments,
// processing instructions and white space around it, the XML declaration
// only at the start, no DOCTYPE, and no attribute given twice.
func scan(b []byte) (*doc, error) {
	if isUTF16(b) {
		return nil, errUTF16
	}
	d := &doc{b: b}
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		d.bom = 3
	}
	dec := xml.NewDecoder(bytes.NewReader(b[d.bom:]))
	dec.Strict = true
	// the bytes are checked as UTF-8 whatever the declaration says, as
	// .NET reads a file without a byte order mark
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var stack []*node
	done := false // the root ended
	for {
		at := d.bom + int(dec.InputOffset())
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		end := d.bom + int(dec.InputOffset())
		switch t := tok.(type) {
		case xml.StartElement:
			if done {
				return nil, fmt.Errorf("a second root element <%s>", t.Name.Local)
			}
			seen := map[xml.Name]bool{}
			for _, a := range t.Attr {
				if seen[a.Name] {
					return nil, fmt.Errorf("<%s> has the attribute %s twice", t.Name.Local, a.Name.Local)
				}
				seen[a.Name] = true
			}
			n := &node{name: t.Name, attr: t.Copy().Attr, start: at, open: end}
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.kids = append(top.kids, n)
			} else {
				d.root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n.close, n.end = at, end
			n.empty = at == end && at == n.open
			if len(stack) == 0 {
				done = true
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			} else if !xmlSpace(string(t)) {
				return nil, errors.New("text outside the root element")
			}
		case xml.ProcInst:
			if t.Target == "xml" && at != d.bom {
				return nil, errors.New("an XML declaration that is not at the start")
			}
		case xml.Directive:
			return nil, errors.New("a DOCTYPE or other declaration")
		}
	}
	if d.root == nil {
		return nil, errors.New("no root element")
	}
	return d, nil
}

// checkRoot: the root element is called local, in no namespace.
func checkRoot(d *doc, local string) error {
	if r := d.root.name; r.Space != "" || r.Local != local {
		if r.Space != "" {
			return fmt.Errorf("the root element is <%s> in namespace %q, where DS4Windows expects <%s>", r.Local, r.Space, local)
		}
		return fmt.Errorf("the root element is <%s>, where DS4Windows expects <%s>", r.Local, local)
	}
	return nil
}

// Devices are the values DS4Windows reads in a rule's device attribute,
// in their exact case.
var Devices = []string{"Any", "DualSense", "DS4", "DS3", "SwitchPro", "JoyCons"}

// xmlBool: the values .NET's XmlConvert reads as a bool attribute, exactly
// as written apart from white space around them.
func xmlBool(v string) bool {
	switch strings.Trim(v, " \t\r\n") {
	case "true", "false", "1", "0":
		return true
	}
	return false
}

// textBool: what .NET's bool.TryParse reads, as DS4Windows reads
// UseDSXUDPServer: true or false in any case.
func textBool(v string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// validAutoProfiles: DS4Windows can read every rule of an "Auto Profiles.xml".
func validAutoProfiles(d *doc) error {
	if err := checkRoot(d, "Programs"); err != nil {
		return err
	}
	n := 0
	for _, p := range d.root.kids {
		if p.name.Space != "" || p.name.Local != "Program" {
			continue // DS4Windows skips elements it does not know
		}
		n++
		if v, ok := p.attrValue("device"); ok && !slices.Contains(Devices, v) {
			return fmt.Errorf("rule %d has device=%q, which DS4Windows does not read", n, v)
		}
		if v, ok := p.attrValue("applyToAllControllers"); ok && !xmlBool(v) {
			return fmt.Errorf("rule %d has applyToAllControllers=%q, which DS4Windows does not read", n, v)
		}
		for _, k := range p.kids {
			if k.name.Space == "" && len(k.kids) > 0 && ruleText(k.name.Local) {
				return fmt.Errorf("rule %d has elements inside <%s>", n, k.name.Local)
			}
		}
	}
	return nil
}

// ruleText: a rule's element DS4Windows reads as text.
func ruleText(local string) bool {
	if local == "TurnOff" {
		return true
	}
	c, ok := strings.CutPrefix(local, "Controller")
	return ok && len(c) == 1 && c[0] >= '1' && c[0] <= '8'
}

// listenerElements are the elements of Profiles.xml EDSense writes.
var listenerElements = []string{"UseDSXUDPServer", "DSXUDPServerPort", "DSXUDPServerListenAddress"}

// validSettings: Profiles.xml has the root DS4Windows reads, and the
// listener's elements, when there, hold text DS4Windows reads: its port
// as digits.
func validSettings(d *doc) error {
	if err := checkRoot(d, "Profile"); err != nil {
		return err
	}
	for _, name := range listenerElements {
		if e := d.root.first(name); e != nil && len(e.kids) > 0 {
			return fmt.Errorf("<%s> holds elements", name)
		}
	}
	if e := d.root.first("UseDSXUDPServer"); e != nil {
		if _, ok := textBool(e.text); !ok {
			return fmt.Errorf("<UseDSXUDPServer> holds %q, which is not true or false", e.text)
		}
	}
	if e := d.root.first("DSXUDPServerPort"); e != nil {
		if _, ok := portText(e.text); !ok {
			return fmt.Errorf("<DSXUDPServerPort> holds %q, which is not a port", e.text)
		}
	}
	return nil
}

// udpElements are the elements of Profiles.xml for DS4Windows' UDP
// server that EDSense writes.
var udpElements = []string{"UseUDPServer", "UDPServerPort", "UDPServerListenAddress"}

// usableAddress: an address of the UDP server EDSense reads it at: empty,
// 0.0.0.0, localhost or an IPv4 loopback address.
func usableAddress(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "0.0.0.0" || strings.EqualFold(s, "localhost") {
		return true
	}
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && ip.IsLoopback() && !strings.Contains(s, ":")
}

// udpPortText is UDPServerPort as DS4Windows reads it: a 32-bit number,
// spaces and a sign allowed, held to 1024..65535. False for text it
// cannot read, which makes it drop the whole file.
func udpPortText(s string) (int, bool) {
	n, err := strconv.ParseInt(strings.Trim(s, " \t\r\n"), 10, 32)
	if err != nil {
		return 0, false
	}
	return int(min(max(n, 1024), 65535)), true
}

// udpServerOn: the UDP server is on, at a port DS4Windows reads and an
// address EDSense reads it at.
func udpServerOn(root *node) bool {
	u := root.first("UseUDPServer")
	if u == nil {
		return false
	}
	if on, ok := textBool(u.text); !on || !ok {
		return false
	}
	if e := root.first("UDPServerPort"); e != nil {
		if _, ok := udpPortText(e.text); !ok {
			return false
		}
	}
	if e := root.first("UDPServerListenAddress"); e != nil && !usableAddress(e.text) {
		return false
	}
	return true
}

// validUDP: Profiles.xml has the root DS4Windows reads, and the UDP
// server's elements, when there, hold text DS4Windows reads: its port as
// a number.
func validUDP(d *doc) error {
	if err := checkRoot(d, "Profile"); err != nil {
		return err
	}
	for _, name := range udpElements {
		if e := d.root.first(name); e != nil && len(e.kids) > 0 {
			return fmt.Errorf("<%s> holds elements", name)
		}
	}
	if e := d.root.first("UseUDPServer"); e != nil {
		if _, ok := textBool(e.text); !ok {
			return fmt.Errorf("<UseUDPServer> holds %q, which is not true or false", e.text)
		}
	}
	if e := d.root.first("UDPServerPort"); e != nil {
		if _, ok := udpPortText(e.text); !ok {
			return fmt.Errorf("<UDPServerPort> holds %q, which is not a number", e.text)
		}
	}
	return nil
}

// checkUDPServer: Profiles.xml reads, and the UDP server is on where
// EDSense reads it.
func checkUDPServer(d *doc) error {
	if err := validUDP(d); err != nil {
		return err
	}
	if !udpServerOn(d.root) {
		return errors.New("the UDP server is not on")
	}
	return nil
}

// portText is a port written as digits only, 1 to 65535.
func portText(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	p, err := strconv.Atoi(s)
	return p, err == nil && p >= 1 && p <= 65535
}

// Enums DS4Windows reads in a profile, in their exact case.
var (
	GyroModes     = []string{"None", "Controls", "Mouse", "MouseJoystick", "DirectionalSwipe", "Passthru"}
	TouchpadModes = []string{"None", "Mouse", "Controls", "MouseJoystick", "AbsoluteMouse", "Passthru"}
)

// validProfile: a profile file is one DS4Windows reads, with the exact
// names of its gyro and touchpad modes.
func validProfile(d *doc) error {
	if err := checkRoot(d, "DS4Windows"); err != nil {
		return err
	}
	for _, c := range []struct {
		name  string
		names []string
	}{{"GyroOutputMode", GyroModes}, {"TouchpadOutputMode", TouchpadModes}} {
		if e := d.root.first(c.name); e != nil && !slices.Contains(c.names, e.text) {
			return fmt.Errorf("<%s> holds %q, which DS4Windows does not read", c.name, e.text)
		}
	}
	if e := d.root.first("Color"); e != nil && !colorText(e.text) {
		return fmt.Errorf("<Color> holds %q, which is not red,green,blue", e.text)
	}
	return nil
}

// colorText: "r,g,b", each a byte.
func colorText(s string) bool {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 255 || strings.TrimSpace(p) != p {
			return false
		}
	}
	return true
}

// validOurProfile: the profile is the one EDSense writes: config_version
// 5, a DualSense, the gyro and touchpad passed through.
func validOurProfile(d *doc) error {
	if err := validProfile(d); err != nil {
		return err
	}
	if v, _ := d.root.attrValue("config_version"); v != ConfigVersion {
		return fmt.Errorf("config_version is %q, not %q", v, ConfigVersion)
	}
	for _, c := range []struct{ name, want string }{
		{"OutputContDevice", OutputDualSense},
		{"GyroOutputMode", "Passthru"},
		{"TouchpadOutputMode", "Passthru"},
	} {
		if e := d.root.first(c.name); e == nil || e.text != c.want {
			return fmt.Errorf("<%s> is not %s", c.name, c.want)
		}
	}
	return nil
}
