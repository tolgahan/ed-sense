package ds4w

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Settings is what EDSense reads from DS4Windows' Profiles.xml.
type Settings struct {
	AppVersion  string        // the DS4Windows that saved the file
	Controllers [slots]string // the profile of each controller slot
	Listener    bool          // "Let game mods control triggers and lights"
	Port        int           // of the DSX listener; 0 when not set
	Address     string
}

type settingsXML struct {
	AppVersion string `xml:"app_version,attr"`
	C1         string `xml:"Controller1"`
	C2         string `xml:"Controller2"`
	C3         string `xml:"Controller3"`
	C4         string `xml:"Controller4"`
	C5         string `xml:"Controller5"`
	C6         string `xml:"Controller6"`
	C7         string `xml:"Controller7"`
	C8         string `xml:"Controller8"`
	Listener   string `xml:"UseDSXUDPServer"`
	Port       string `xml:"DSXUDPServerPort"`
	Address    string `xml:"DSXUDPServerListenAddress"`
}

// ReadSettings reads Profiles.xml in DS4Windows' data folder.
func ReadSettings(dataDir string) (Settings, error) {
	var x settingsXML
	if err := readXML(filepath.Join(dataDir, settingsFile), &x); err != nil {
		return Settings{}, err
	}
	s := Settings{
		AppVersion:  x.AppVersion,
		Controllers: [slots]string{x.C1, x.C2, x.C3, x.C4, x.C5, x.C6, x.C7, x.C8},
		Listener:    strings.EqualFold(strings.TrimSpace(x.Listener), "true"),
		Address:     strings.TrimSpace(x.Address),
	}
	for i := range s.Controllers {
		s.Controllers[i] = strings.TrimSpace(s.Controllers[i])
	}
	if p, err := strconv.Atoi(strings.TrimSpace(x.Port)); err == nil {
		s.Port = p
	}
	return s, nil
}

// AutoProfile is one rule of "Auto Profiles.xml": the profiles to use
// while a program is in front.
type AutoProfile struct {
	Path, Title string
	Device      string // the kind of controller it is for: "" (any), or one such as "DualSense" or "DS4"
	All         bool   // Controller1's profile for every slot
	Profiles    [slots]string
}

type autoProfilesXML struct {
	Programs []struct {
		Path   string `xml:"path,attr"`
		Title  string `xml:"title,attr"`
		Device string `xml:"device,attr"`
		All    string `xml:"applyToAllControllers,attr"`
		C1     string `xml:"Controller1"`
		C2     string `xml:"Controller2"`
		C3     string `xml:"Controller3"`
		C4     string `xml:"Controller4"`
		C5     string `xml:"Controller5"`
		C6     string `xml:"Controller6"`
		C7     string `xml:"Controller7"`
		C8     string `xml:"Controller8"`
	} `xml:"Program"`
}

// ReadAutoProfiles reads "Auto Profiles.xml" in DS4Windows' data folder.
func ReadAutoProfiles(dataDir string) ([]AutoProfile, error) {
	var x autoProfilesXML
	if err := readXML(filepath.Join(dataDir, autoProfilesFile), &x); err != nil {
		return nil, err
	}
	var out []AutoProfile
	for _, p := range x.Programs {
		out = append(out, AutoProfile{
			Path: strings.TrimSpace(p.Path), Title: strings.TrimSpace(p.Title), Device: strings.TrimSpace(p.Device),
			All:      strings.EqualFold(strings.TrimSpace(p.All), "true"),
			Profiles: [slots]string{p.C1, p.C2, p.C3, p.C4, p.C5, p.C6, p.C7, p.C8},
		})
	}
	return out, nil
}

// matches: the rule's path picks the program at exe, a full path, or only
// the exe's name when its path is unknown. As in DS4Windows: "^" at the
// start matches the start, "$" at the end the end, "*" at the start any
// part, else the whole path; case and slashes do not count. A rule with a
// window title only cannot be checked here and does not match.
func (r AutoProfile) matches(exe string) bool {
	norm := func(s string) string { return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "/", `\`) }
	path, want := norm(r.Path), norm(exe)
	if path == "" || want == "" {
		return false
	}
	if !strings.Contains(want, `\`) {
		core := path
		switch {
		case len(core) >= 2 && (core[0] == '^' || core[0] == '*'):
			core = core[1:]
		case len(core) >= 2 && core[len(core)-1] == '$':
			core = core[:len(core)-1]
		}
		return core == want || strings.HasSuffix(core, `\`+want)
	}
	switch {
	case len(path) >= 2 && path[0] == '^':
		return strings.HasPrefix(want, path[1:])
	case len(path) >= 2 && path[len(path)-1] == '$':
		return strings.HasSuffix(want, path[:len(path)-1])
	case len(path) >= 2 && path[0] == '*':
		return strings.Contains(want, path[1:])
	}
	return path == want
}

// forDevice is which pass of DS4Windows' pick a rule belongs to, for the
// DualSense EDSense drives: "DualSense", "Any", or "" for rules for other
// controllers.
func (r AutoProfile) forDevice() string {
	switch d := strings.TrimSpace(r.Device); {
	case d == "" || strings.EqualFold(d, "Any"):
		return "Any"
	case strings.EqualFold(d, "DualSense"):
		return "DualSense"
	}
	return ""
}

// profileFor is the rule's entry for slot; "" when it leaves the
// controller the profile it has (an empty entry or "(none)").
func (r AutoProfile) profileFor(slot int) string {
	p := r.Profiles[slot]
	if r.All {
		p = r.Profiles[0]
	}
	if p = strings.TrimSpace(p); p == "(none)" {
		return ""
	}
	return p
}

// AutoProfileFor is the profile the rules give the DualSense in slot
// while exe is in front, picked as DS4Windows picks it: the first
// matching rule for a DualSense, else the first matching rule for any
// controller, and only that rule. name is "" when that leaves the
// controller its own profile: no rule matches, or the rule's entry is
// empty or "(none)". A rule with a window title as well matches only
// while the title does, which EDSense does not see, so the rules after it
// may decide instead; ok is false when that could change the answer.
func AutoProfileFor(rules []AutoProfile, exe string, slot int) (name string, ok bool) {
	if slot < 0 || slot >= slots {
		return "", true
	}
	could := map[string]bool{} // the answers the rules may give
	decide := func() (string, bool) {
		if len(could) != 1 {
			return "", false
		}
		for name := range could {
			return name, true
		}
		return "", false
	}
	for _, pass := range []string{"DualSense", "Any"} {
		for _, r := range rules {
			if r.forDevice() != pass || !r.matches(exe) {
				continue
			}
			could[r.profileFor(slot)] = true
			if r.Title == "" {
				return decide()
			}
		}
	}
	could[""] = true // no rule may match
	return decide()
}

// ReadLinkedProfiles reads LinkedProfiles.xml in DS4Windows' data folder:
// the profile linked to each controller ("Link profile"), by the
// controller's MAC address without its colons.
func ReadLinkedProfiles(dataDir string) (map[string]string, error) {
	var x anyXML
	if err := readXML(filepath.Join(dataDir, linkedProfilesFile), &x); err != nil {
		return nil, err
	}
	links := map[string]string{}
	if x.XMLName.Local != "LinkedControllers" {
		return links, nil
	}
	for _, e := range x.Items {
		// as DS4Windows reads it: every element with MAC in its name
		if strings.Contains(e.XMLName.Local, "MAC") {
			links[strings.ReplaceAll(e.XMLName.Local, "MAC", "")] = e.Text
		}
	}
	return links, nil
}

// LinkedProfile is the profile linked to the controller with this MAC
// address, as DS4Windows reports it ("AA:BB:CC:DD:EE:FF"), or "".
func LinkedProfile(links map[string]string, mac string) string {
	mac = strings.TrimSpace(mac)
	if mac == "" || mac == blankMAC {
		return "" // DS4Windows links no profile to it
	}
	return strings.TrimSpace(links[strings.ReplaceAll(mac, ":", "")])
}

// blankMAC is DS4Windows' MAC address for a controller that told none.
const blankMAC = "00:00:00:00:00:00"

// Profile is what EDSense reads from a DS4Windows profile.
type Profile struct {
	GyroOutput string // GyroOutputMode, or by an older profile's UseSAforMouse; "Controls" when the file has neither
	GyroMapped bool   // a gyro direction is on a stick or the mouse
	Output     string // the emulated controller, OutputContDevice
	TriggerLab bool   // Trigger Lab sets a trigger, or turns game rumble into trigger vibration
	Touchpad   string // TouchpadOutputMode, or by an older profile's UseTPforControls; "Mouse" when the file has neither
}

// TouchpadMouse: the profile moves the cursor with the touchpad (Output
// Mode Mouse, the default, Mouse Joystick or Absolute Mouse).
func (p Profile) TouchpadMouse() bool {
	switch strings.ToLower(p.Touchpad) {
	case "mouse", "mousejoystick", "absolutemouse":
		return true
	}
	return false
}

type anyXML struct {
	XMLName xml.Name
	Text    string   `xml:",chardata"`
	Items   []anyXML `xml:",any"`
}

type profileXML struct {
	GyroOutput string  `xml:"GyroOutputMode"`
	SAforMouse *string `xml:"UseSAforMouse"` // from DS4Windows before gyro output modes; wins when there
	Output     string  `xml:"OutputContDevice"`
	Touchpad   string  `xml:"TouchpadOutputMode"`
	TPControls *string `xml:"UseTPforControls"` // from DS4Windows before touchpad output modes; wins when there
	Control    struct {
		Button anyXML `xml:"Button"`
	} `xml:"Control"`
	ShiftControl struct {
		Button anyXML `xml:"Button"`
	} `xml:"ShiftControl"`
	TriggerLab struct {
		Enabled                  string
		LeftActive               string
		RightActive              string
		LeftGameRumbleVibration  string
		RightGameRumbleVibration string
	} `xml:"TriggerLab"`
}

// ReadProfile reads a profile file.
func ReadProfile(path string) (Profile, error) {
	var x profileXML
	if err := readXML(path, &x); err != nil {
		return Profile{}, err
	}
	yes := func(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }
	lab := x.TriggerLab
	p := Profile{
		GyroOutput: strings.TrimSpace(x.GyroOutput),
		Output:     strings.TrimSpace(x.Output),
		// as DS4Windows 5 applies it to each native report: a set trigger,
		// or game rumble as trigger vibration (Off at no rumble)
		TriggerLab: yes(lab.Enabled) && (yes(lab.LeftActive) || yes(lab.RightActive) ||
			yes(lab.LeftGameRumbleVibration) || yes(lab.RightGameRumbleVibration)),
	}
	switch {
	case x.SAforMouse != nil:
		// as DS4Windows reads an older profile: this element decides the
		// output, and anything but true reads as false
		p.GyroOutput = "Controls"
		if yes(*x.SAforMouse) {
			p.GyroOutput = "Mouse"
		}
	case p.GyroOutput == "":
		p.GyroOutput = "Controls" // DS4Windows' default
	}
	// the touchpad the same way (ProfileDTO.cs, UseTPforControls)
	p.Touchpad = strings.TrimSpace(x.Touchpad)
	switch {
	case x.TPControls != nil:
		p.Touchpad = "Mouse"
		if yes(*x.TPControls) {
			p.Touchpad = "Controls"
		}
	case p.Touchpad == "":
		p.Touchpad = "Mouse" // DS4Windows' default
	}
	for _, b := range append(x.Control.Button.Items, x.ShiftControl.Button.Items...) {
		switch b.XMLName.Local {
		case "GyroXPos", "GyroXNeg", "GyroZPos", "GyroZNeg":
			p.GyroMapped = p.GyroMapped || stickOrMouse(b.Text)
		}
	}
	return p, nil
}

// stickOrMouse: a button mapping's output is a stick direction or mouse
// movement, by its name or its display name.
func stickOrMouse(v string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), " ", ""))
	switch k {
	case "lxneg", "lxpos", "lyneg", "lypos", "rxneg", "rxpos", "ryneg", "rypos",
		"mouseup", "mousedown", "mouseleft", "mouseright",
		"absmouseup", "absmousedown", "absmouseleft", "absmouseright",
		"leftstickleft", "leftstickright", "leftstickup", "leftstickdown",
		"rightstickleft", "rightstickright", "rightstickup", "rightstickdown":
		return true
	}
	return strings.Contains(k, "axis")
}

// Gyro is what a profile does with the gyro, as far as EDSense's own gyro
// aim cares.
type Gyro int

const (
	GyroUnknown Gyro = iota // the profile could not be read
	GyroFree                // nothing: output None or Passthru, or Controls with no gyro on a stick or the mouse
	GyroMouse               // gyro to mouse
	GyroOther               // a stick (Mouse Joystick, or Controls on a stick or the mouse), or swipes
)

func (g Gyro) String() string {
	switch g {
	case GyroFree:
		return "free"
	case GyroMouse:
		return "mouse"
	case GyroOther:
		return "in use"
	}
	return "unknown"
}

// Gyro is what the profile does with the gyro.
func (p Profile) Gyro() Gyro {
	switch strings.ToLower(p.GyroOutput) {
	case "none", "passthru":
		return GyroFree
	case "controls":
		if p.GyroMapped {
			return GyroOther
		}
		return GyroFree
	case "mouse":
		return GyroMouse
	case "mousejoystick", "directionalswipe":
		return GyroOther
	}
	return GyroUnknown
}

// EmulatesDualSense: the output is a DualSense (or an Edge), by the
// profile's OutputContDevice or DS4Windows' own name for it.
func EmulatesDualSense(output string) bool {
	return strings.Contains(strings.ToLower(strings.ReplaceAll(output, " ", "")), "dualsense")
}

var errAmbiguous = errors.New("more than one profile matches the name")

// FindProfile is the file of the profile called name. DS4Windows answers
// in ASCII, with '?' for each character outside it, so such a name is
// matched against the files, and must match exactly one.
func FindProfile(dataDir, name string) (string, error) {
	dir := filepath.Join(dataDir, profilesDir)
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("no profile name %q", name)
	}
	if !strings.Contains(name, "?") {
		path := filepath.Join(dir, name+".xml")
		if !exists(path) {
			return "", fmt.Errorf("%s not found", path)
		}
		return path, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range entries {
		base, ok := cutSuffixFold(e.Name(), ".xml")
		if ok && !e.IsDir() && asciiMatch(name, base) {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no profile in %s matches %q", dir, name)
	case 1:
		return found[0], nil
	}
	return "", errAmbiguous
}

func cutSuffixFold(s, suffix string) (string, bool) {
	if len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix) {
		return s[:len(s)-len(suffix)], true
	}
	return s, false
}

// asciiMatch: ascii is real as DS4Windows sends it, each character
// outside ASCII turned into '?'.
func asciiMatch(ascii, real string) bool {
	i := 0
	for _, r := range real {
		if i >= len(ascii) {
			return false
		}
		if r < 0x80 && ascii[i] != byte(r) || r >= 0x80 && ascii[i] != '?' {
			return false
		}
		i++
	}
	return i == len(ascii)
}

// readXML decodes a DS4Windows XML file, in UTF-8 or UTF-16.
func readXML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	d := xml.NewDecoder(bytes.NewReader(utf8Text(b)))
	// the text is UTF-8 by now, whatever the declaration says
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// utf8Text drops a UTF-8 byte order mark, and turns UTF-16 with one into
// UTF-8.
func utf8Text(b []byte) []byte {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return b[3:]
	case len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF):
		big := b[0] == 0xFE
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if big {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
			}
		}
		return []byte(string(utf16.Decode(u)))
	}
	return b
}
