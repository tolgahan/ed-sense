package ds4w

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
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

// The installer: EDSense's own DS4Windows profile for Elite, an Auto
// Profiles rule that loads it while Elite is in front, and game mod
// support and its UDP server turned on. It edits DS4Windows' files only while DS4Windows is
// closed, keeps copies first, and never touches a rule the player made
// for Elite.

// ProfileName is the DS4Windows profile EDSense writes for Elite.
const ProfileName = "Elite Dangerous (EDSense)"

// RulePath is the path of EDSense's Auto Profiles rule: an exe called
// EliteDangerous64.exe, wherever the game is installed.
const RulePath = "EliteDangerous64.exe$"

const (
	ConfigVersion   = "5"               // of the profiles DS4Windows 5 writes
	OutputDualSense = "ViiperDualSense" // DS4Windows' name for DualSense emulation

	eliteExe       = "EliteDangerous64.exe"
	labData        = "lab-data" // the portable lab's data folder
	defaultAddress = "127.0.0.1"
	defaultVersion = "5.0.0.0"
	ourFile        = profilesDir + `\` + ProfileName + ".xml" // as the player sees it
)

// Steps, in the order they are written.
const (
	StepProfile   = "profile"    // Profiles\Elite Dangerous (EDSense).xml
	StepRule      = "rule"       // the rule in Auto Profiles.xml
	StepListener  = "listener"   // game mod support on in Profiles.xml
	StepUDPServer = "udp_server" // Settings > UDP Server > Enable Server on in Profiles.xml
)

// Step states.
const (
	StepTodo = "todo"
	StepDone = "done"
	StepSkip = "skip" // not EDSense's to do
)

// Plan states.
const (
	PlanBlocked = "blocked"  // EDSense leaves DS4Windows' files alone: see Block
	PlanMissing = "missing"  // every step is to do
	PlanPartial = "partial"  // some steps are done, some to do
	PlanOurs    = "ours"     // Elite has EDSense's rule and profile, and game mod support and the UDP server are on
	PlanOther   = "other"    // the player has a rule for Elite: only the listener and the UDP server are EDSense's
	PlanUsualOK = "usual_ok" // no rule for Elite, and the profile the controller has already works
)

// Why a plan is blocked.
const (
	BlockFolder     = "folder"     // no data folder, or Profiles.xml, Auto Profiles.xml or Profiles\ missing
	BlockOld        = "old"        // a DS4Windows before 5, without game mod support
	BlockLab        = "lab"        // DS4Windows' portable lab
	BlockUnreadable = "unreadable" // a file EDSense cannot read for sure: UTF-16, broken, or one DS4Windows could not read
	BlockTwo        = "two"        // DS4Windows has settings in two folders, and asks at each start which one to use
)

// Input is what Inspect looks at.
type Input struct {
	Dir     string   // DS4Windows' data folder; "" when not known
	ExeDir  string   // the folder of DS4Windows' exe when known, for its portable lab
	Elite   []string // Elite's exe paths known; its bare name is always added
	Version string   // DS4Windows' version, used when Profiles.xml names none; "" when not known
	Slot    int      // the controller's DS4Windows slot, from 0
	Usual   string   // the controller's profile when no rule applies, as DS4Windows told it; "" reads Profiles.xml
	// Also is a second folder with DS4Windows' settings (its exe's folder
	// and %APPDATA%\DS4Windows both): DS4Windows then asks at each start
	// which one to use, so EDSense cannot tell; "" when there is none.
	Also string
}

// Step is one part of the install.
type Step struct {
	ID    string // StepProfile, StepRule, StepListener or StepUDPServer
	State string // StepTodo, StepDone or StepSkip
}

// Rule is the player's own Auto Profiles rule for Elite.
type Rule struct {
	Index       int // from 1, in the file's order
	Path, Title string
	Profile     string // the profile it loads; "" when it leaves the controller its own
}

// Facts are what EDSense reads in a DS4Windows profile.
type Facts struct {
	Name       string // "" when not known
	Problem    string // why it was not read; "" when it was
	Read       bool   // the file was read
	Output     string // the emulated controller
	Gyro       Gyro
	Touchpad   string
	TriggerLab bool
}

// Works: the profile emulates a DualSense, leaves the gyro free, does not
// move the cursor with the touchpad, and has Trigger Lab off.
func (f Facts) Works() bool {
	return f.Read && EmulatesDualSense(f.Output) && f.Gyro == GyroFree &&
		!(Profile{Touchpad: f.Touchpad}).TouchpadMouse() && !f.TriggerLab
}

// Plan is what Inspect found, and what an install would do.
type Plan struct {
	Dir     string
	State   string
	Block   string // why it is blocked
	File    string // the file it is about, relative to Dir
	Why     string // for BlockUnreadable: what is wrong with File
	Version string // DS4Windows' version; "" when not known

	Steps     []Step // profile, rule, listener and udp_server, in that order; none when blocked
	Rule      *Rule  // the player's rule for Elite (PlanOther)
	RuleLoses bool   // Rule is there, but DS4Windows picks EDSense's rule for the DualSense
	Player    Facts  // the profile that rule loads
	Usual     Facts  // the profile the controller has while no rule applies
	Endpoint  string // where game mod support listens once it is on, such as "127.0.0.1:6969"
	Exclusive bool   // "Use HidHide to Prevent Double Input" is on

	UDPEndpoint string // where the UDP server listens once EDSense has turned it on, such as "127.0.0.1:26760"
	UDPMoves    string // the address an install replaces with 127.0.0.1 (one EDSense does not use); "": none
}

// Step is the state of the step id, "" when the plan has none.
func (p Plan) Step(id string) string {
	for _, s := range p.Steps {
		if s.ID == id {
			return s.State
		}
	}
	return ""
}

// Todo is the steps an install would write, in order. A reset writes the
// profile again, when it is EDSense's to write.
func (p Plan) Todo(reset bool) []string {
	var ids []string
	for _, s := range p.Steps {
		switch {
		case reset && s.ID == StepProfile && s.State != StepSkip:
			ids = append(ids, s.ID)
		case !reset && s.State == StepTodo:
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// Files are the files an install writes, as named in the data folder,
// each once.
func (p Plan) Files(reset bool) []string {
	var files []string
	for _, id := range p.Todo(reset) {
		if f := StepFile(id); !slices.Contains(files, f) {
			files = append(files, f)
		}
	}
	return files
}

// StepFile is the file a step writes, as named in the data folder.
func StepFile(id string) string {
	switch id {
	case StepProfile:
		return ourFile
	case StepRule:
		return autoProfilesFile
	case StepListener, StepUDPServer:
		return settingsFile
	}
	return ""
}

// ShowDir names a data folder for the window without the player's user
// name: %APPDATA%\... or %USERPROFILE%\... when it is in one of those,
// else as it is.
func ShowDir(dir, appData, home string) string {
	for _, root := range []struct{ path, name string }{{appData, "%APPDATA%"}, {home, "%USERPROFILE%"}} {
		if root.path == "" {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(root.path), filepath.Clean(dir))
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			if rel == "." {
				return root.name
			}
			return root.name + `\` + strings.ReplaceAll(rel, "/", `\`)
		}
	}
	return dir
}

// CanInstall: some steps are to do.
func (p Plan) CanInstall() bool { return len(p.Todo(false)) > 0 }

// CanReset: EDSense's profile is there and Elite's rule loads it.
func (p Plan) CanReset() bool {
	return p.Step(StepRule) == StepDone && p.Step(StepProfile) == StepDone
}

// BlockedError is why EDSense leaves DS4Windows' files alone.
type BlockedError struct{ Plan Plan }

func (e *BlockedError) Error() string {
	p := e.Plan
	switch p.Block {
	case BlockOld:
		if p.Version == "" {
			return "this DS4Windows has no game mod support"
		}
		return fmt.Sprintf("DS4Windows %s has no game mod support", p.Version)
	case BlockLab:
		return "this is DS4Windows' portable lab"
	case BlockTwo:
		return "DS4Windows has settings in two folders, and EDSense cannot tell which one it uses"
	case BlockUnreadable:
		return fmt.Sprintf("%s could not be read (%s)", p.File, p.Why)
	}
	if p.File != "" {
		return fmt.Sprintf("DS4Windows' settings were not found (no %s)", p.File)
	}
	return "DS4Windows' settings were not found"
}

// files are the bytes Inspect read, which an install backs up and edits.
type files struct {
	settings, auto []byte
	ours           []byte // nil when EDSense's profile is missing
	oursErr        error  // why it could not be read
	appVersion     string // Profiles.xml's
	programs       int    // the rules in Auto Profiles.xml
}

// Inspect reads DS4Windows' files in in.Dir and tells what is set up for
// Elite and what an install would write. It writes nothing.
func Inspect(in Input) Plan {
	p, _ := inspect(in)
	return p
}

func inspect(in Input) (Plan, *files) {
	p := Plan{Dir: in.Dir, Version: strings.TrimSpace(in.Version)}
	block := func(kind, file, why string) (Plan, *files) {
		p.State, p.Block, p.File, p.Why = PlanBlocked, kind, file, why
		return p, nil
	}
	dir := in.Dir
	if dir == "" {
		return block(BlockFolder, "", "")
	}
	if strings.EqualFold(filepath.Base(dir), labData) ||
		in.ExeDir != "" && exists(filepath.Join(in.ExeDir, labData, autoProfilesFile)) {
		return block(BlockLab, "", "")
	}
	if in.Also != "" {
		return block(BlockTwo, "", "")
	}
	for _, name := range []string{settingsFile, autoProfilesFile, profilesDir} {
		st, err := os.Stat(filepath.Join(dir, name))
		switch {
		case errors.Is(err, fs.ErrNotExist) || err == nil && st.IsDir() != (name == profilesDir):
			return block(BlockFolder, name, "")
		case err != nil:
			return block(BlockUnreadable, name, plainErr(err))
		}
	}

	f := &files{}
	var err error
	if f.settings, err = os.ReadFile(filepath.Join(dir, settingsFile)); err != nil {
		return block(BlockUnreadable, settingsFile, plainErr(err))
	}
	sd, err := scan(f.settings)
	if err == nil {
		err = checkRoot(sd, "Profile")
	}
	if err != nil {
		return block(BlockUnreadable, settingsFile, err.Error())
	}
	set, err := parseSettings(f.settings)
	if err != nil {
		return block(BlockUnreadable, settingsFile, err.Error())
	}
	f.appVersion = strings.TrimSpace(set.AppVersion)
	if f.appVersion != "" {
		p.Version = f.appVersion
	}
	if major, known := Major(p.Version); known && major < 5 || !known && sd.root.first("UseDSXUDPServer") == nil {
		return block(BlockOld, "", "")
	}

	if f.auto, err = os.ReadFile(filepath.Join(dir, autoProfilesFile)); err != nil {
		return block(BlockUnreadable, autoProfilesFile, plainErr(err))
	}
	ad, err := scan(f.auto)
	if err == nil {
		err = validAutoProfiles(ad)
	}
	if err != nil {
		return block(BlockUnreadable, autoProfilesFile, err.Error())
	}
	rules, err := parseAutoProfiles(f.auto)
	if err != nil {
		return block(BlockUnreadable, autoProfilesFile, err.Error())
	}
	f.programs = len(rules)

	oursPath := filepath.Join(dir, profilesDir, ProfileName+".xml")
	have := exists(oursPath)
	if have {
		f.ours, f.oursErr = os.ReadFile(oursPath)
		if f.ours == nil && f.oursErr == nil {
			f.ours = []byte{}
		}
	}

	p.Exclusive = set.Exclusive
	port, addr := endpointAfter(sd.root)
	p.Endpoint = net.JoinHostPort(addr, strconv.Itoa(port))
	p.UDPEndpoint, p.UDPMoves = udpAfter(sd.root)
	p.Usual = usualFacts(dir, in, set)

	elite := append(slices.Clone(in.Elite), eliteExe)
	ours := false
	for i, r := range rules {
		switch {
		case isOurs(r):
			ours = true
		case p.Rule == nil && forElite(r, elite):
			p.Rule = &Rule{Index: i + 1, Path: r.Path, Title: r.Title, Profile: ruleProfile(r)}
		}
	}
	// DS4Windows picks, for the DualSense, the first rule made for a
	// DualSense, else the first for any controller, whatever their order:
	// EDSense's rule, made for a DualSense, beats the player's for any.
	if w := pickForDualSense(rules, func(r AutoProfile) bool { return isOurs(r) || forElite(r, elite) }); w >= 0 && p.Rule != nil {
		if r := rules[w]; isOurs(r) {
			p.RuleLoses = true
		} else {
			p.Rule = &Rule{Index: w + 1, Path: r.Path, Title: r.Title, Profile: ruleProfile(r)}
		}
	}

	profile, rule, listener, udp := StepTodo, StepTodo, StepTodo, StepTodo
	if have {
		profile = StepDone
	}
	if listenerOn(sd.root) {
		listener = StepDone
	}
	if udpServerOn(sd.root) {
		udp = StepDone
	}
	switch {
	case p.Rule != nil && !p.RuleLoses:
		p.State, profile, rule = PlanOther, StepSkip, StepSkip
		p.Player = profileFacts(dir, p.Rule.Profile)
	case ours:
		rule = StepDone
	case p.Usual.Works():
		p.State, profile, rule = PlanUsualOK, StepSkip, StepSkip
	}
	p.Steps = []Step{{StepProfile, profile}, {StepRule, rule}, {StepListener, listener}, {StepUDPServer, udp}}
	if p.State == "" {
		switch n := len(p.Todo(false)); n {
		case 0:
			p.State = PlanOurs
		case len(p.Steps):
			p.State = PlanMissing
		default:
			p.State = PlanPartial
		}
	}
	return p, f
}

// isOurs: the rule is the one EDSense writes, loading its profile.
func isOurs(r AutoProfile) bool {
	return strings.EqualFold(r.Path, RulePath) && strings.EqualFold(strings.TrimSpace(r.Profiles[0]), ProfileName)
}

// pickForDualSense is the index of the rule DS4Windows picks for a
// DualSense among those want takes: the first one for a DualSense, else
// the first one for any controller; -1 when there is none.
func pickForDualSense(rules []AutoProfile, want func(AutoProfile) bool) int {
	for _, pass := range []string{"DualSense", "Any"} {
		for i, r := range rules {
			if r.forDevice() == pass && want(r) {
				return i
			}
		}
	}
	return -1
}

// forElite: the rule may be the player's for Elite. It matches one of
// Elite's paths or its bare name, or its path ends in the exe's name
// once ^, $ and * are taken off, or its path or title names Elite. A
// false match only keeps EDSense from adding its rule.
func forElite(r AutoProfile, elite []string) bool {
	for _, e := range elite {
		if r.matches(e) {
			return true
		}
	}
	path := strings.ToLower(strings.ReplaceAll(r.Path, "/", `\`))
	if strings.HasSuffix(strings.Trim(path, "^$*"), strings.ToLower(eliteExe)) {
		return true
	}
	return strings.Contains(path, "elite") || strings.Contains(strings.ToLower(r.Title), "elite")
}

// ruleProfile is the profile a rule loads, as the card names it: its
// first entry, else the first entry that names one.
func ruleProfile(r AutoProfile) string {
	for _, p := range r.Profiles {
		if p = strings.TrimSpace(p); p != "" && p != "(none)" {
			return p
		}
	}
	return ""
}

// usualFacts is the profile the controller has while no rule applies:
// the one DS4Windows told, else the one Profiles.xml gives its slot. A
// profile linked to the controller cannot be told without its address.
func usualFacts(dir string, in Input, set Settings) Facts {
	name := strings.TrimSpace(in.Usual)
	if name == "" {
		if links, err := ReadLinkedProfiles(dir); err == nil && len(links) > 0 {
			return Facts{Problem: "a profile may be linked to the controller"}
		}
		slot := in.Slot
		if slot < 0 || slot >= slots {
			slot = 0
		}
		if name = set.Controllers[slot]; name == "" {
			return Facts{Problem: settingsFile + " names no profile"}
		}
	}
	return profileFacts(dir, name)
}

// profileFacts reads the profile called name.
func profileFacts(dir, name string) Facts {
	f := Facts{Name: name}
	if name == "" {
		return f
	}
	path, err := FindProfile(dir, name)
	if err != nil {
		f.Problem = "its file was not found"
		return f
	}
	p, err := ReadProfile(path)
	if err != nil {
		f.Problem = "its file could not be read"
		return f
	}
	f.Read, f.Output, f.Gyro, f.Touchpad, f.TriggerLab = true, p.Output, p.Gyro(), p.Touchpad, p.TriggerLab
	return f
}

// listenerOn: game mod support is on, at an address DS4Windows takes.
func listenerOn(root *node) bool {
	u := root.first("UseDSXUDPServer")
	if u == nil {
		return false
	}
	on, ok := textBool(u.text)
	if !on || !ok {
		return false
	}
	if e := root.first("DSXUDPServerPort"); e != nil {
		if _, ok := portText(e.text); !ok {
			return false
		}
	}
	if e := root.first("DSXUDPServerListenAddress"); e != nil && !loopbackText(e.text) {
		return false
	}
	return true
}

// loopbackText: a loopback address, written without spaces.
func loopbackText(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.IsLoopback()
}

// endpointAfter is where the listener listens once EDSense has turned it
// on: the file's port and address where valid, else DS4Windows' own.
func endpointAfter(root *node) (int, string) {
	port, addr := DefaultPort, defaultAddress
	if e := root.first("DSXUDPServerPort"); e != nil {
		if p, ok := portText(e.text); ok {
			port = p
		}
	}
	if e := root.first("DSXUDPServerListenAddress"); e != nil && loopbackText(e.text) {
		addr = e.text
	}
	return port, addr
}

// udpAfter is where the UDP server listens once EDSense has turned it on:
// the file's port as DS4Windows reads it, else DS4Windows' own, at the
// address EDSense reads it at. moves is the address an install replaces,
// one EDSense does not use; "" when there is none.
func udpAfter(root *node) (endpoint, moves string) {
	s := Settings{UDPPort: DefaultUDPPort}
	if e := root.first("UDPServerPort"); e != nil {
		if p, ok := udpPortText(e.text); ok {
			s.UDPPort = p
		}
	}
	if e := root.first("UDPServerListenAddress"); e != nil {
		if usableAddress(e.text) {
			s.UDPAddress = e.text
		} else {
			moves = strings.TrimSpace(e.text)
		}
	}
	addr, _, _ := s.UDPEndpoint()
	return addr.String(), moves
}

// ProfileFile is EDSense's profile: DS4Windows' defaults but for the
// lightbar, DualSense emulation, and the gyro and touchpad passed
// through. appVersion is the app_version of Profiles.xml.
func ProfileFile(appVersion string) []byte {
	if !versionText(appVersion) {
		appVersion = defaultVersion
	}
	lines := []string{
		`<?xml version="1.0" encoding="utf-8"?>`,
		`<!-- Written by EDSense for Elite Dangerous. Every setting not listed here is DS4Windows' default. -->`,
		`<DS4Windows app_version="` + appVersion + `" config_version="` + ConfigVersion + `">`,
		`  <Color>0,0,255</Color>`,
		`  <OutputContDevice>` + OutputDualSense + `</OutputContDevice>`,
		`  <GyroOutputMode>Passthru</GyroOutputMode>`,
		`  <TouchpadOutputMode>Passthru</TouchpadOutputMode>`,
		`</DS4Windows>`,
	}
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

// versionText: a version such as 5.0.12.0, digits and dots only.
func versionText(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 5 || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

// ruleLines is EDSense's rule, indented for the root's children.
func ruleLines() []string {
	lines := []string{
		`  <Program path="` + RulePath + `" title="" device="DualSense" applyToAllControllers="true">`,
		`    <Controller1>` + ProfileName + `</Controller1>`,
	}
	for i := 2; i <= slots; i++ {
		lines = append(lines, fmt.Sprintf(`    <Controller%d>(none)</Controller%d>`, i, i))
	}
	return append(lines, `    <TurnOff>False</TurnOff>`, `  </Program>`)
}

// edit replaces b[from:to] with text.
type edit struct {
	from, to int
	text     string
}

// apply makes the edits, which must not overlap.
func apply(b []byte, edits []edit) []byte {
	slices.SortFunc(edits, func(x, y edit) int { return y.from - x.from })
	out := slices.Clone(b)
	for _, e := range edits {
		out = slices.Concat(out[:e.from], []byte(e.text), out[e.to:])
	}
	return out
}

// addChildren is the edit that puts lines in as the last children of n:
// on lines of their own before its end tag, or into an empty <n/>, which
// is opened. eol is the file's line end.
func addChildren(b []byte, n *node, lines []string, eol string) edit {
	body := strings.Join(lines, eol) + eol
	if n.empty {
		head := bytes.TrimRight(b[n.start:n.open-2], " \t\r\n") // "<Programs" and its attributes, without "/>"
		return edit{n.start, n.open, string(head) + ">" + eol + body + "</" + n.name.Local + ">"}
	}
	lineStart := bytes.LastIndexByte(b[:n.close], '\n') + 1
	if lineStart > n.open && xmlSpace(string(b[lineStart:n.close])) {
		return edit{lineStart, lineStart, body}
	}
	return edit{n.close, n.close, eol + body}
}

// SpliceRule puts EDSense's rule into the bytes of "Auto Profiles.xml" as
// the root's last child. The rest of the file stays as it was, byte for
// byte: its line ends, its byte order mark, its comments.
func SpliceRule(b []byte) ([]byte, error) {
	d, err := scan(b)
	if err == nil {
		err = validAutoProfiles(d)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", autoProfilesFile, err)
	}
	return apply(b, []edit{addChildren(b, d.root, ruleLines(), d.eol())}), nil
}

// SetListener turns game mod support on in the bytes of Profiles.xml: the
// first <UseDSXUDPServer> holds True, or one is added. A port or address
// DS4Windows would refuse becomes its default. The rest stays as it was,
// byte for byte.
func SetListener(b []byte) ([]byte, error) {
	d, err := scan(b)
	if err == nil {
		err = checkRoot(d, "Profile")
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", settingsFile, err)
	}
	root := d.root
	var edits []edit
	set := func(n *node, text string) {
		if n.empty {
			edits = append(edits, edit{n.start, n.end, "<" + n.name.Local + ">" + text + "</" + n.name.Local + ">"})
		} else {
			edits = append(edits, edit{n.open, n.close, text})
		}
	}
	if u := root.first("UseDSXUDPServer"); u != nil {
		set(u, "True")
	} else {
		edits = append(edits, addChildren(b, root, []string{"  <UseDSXUDPServer>True</UseDSXUDPServer>"}, d.eol()))
	}
	if e := root.first("DSXUDPServerPort"); e != nil {
		if _, ok := portText(e.text); !ok {
			set(e, strconv.Itoa(DefaultPort))
		}
	}
	if e := root.first("DSXUDPServerListenAddress"); e != nil && !loopbackText(e.text) {
		set(e, defaultAddress)
	}
	return apply(b, edits), nil
}

// SetUDPServer turns DS4Windows' UDP server on in the bytes of
// Profiles.xml: the first <UseUDPServer> holds True, or one is added. A
// port DS4Windows would refuse becomes 26760, and an address EDSense does
// not read it at becomes 127.0.0.1; a port or address the file does not
// have stays DS4Windows' default. The rest stays as it was, byte for
// byte.
func SetUDPServer(b []byte) ([]byte, error) {
	d, err := scan(b)
	if err == nil {
		err = checkRoot(d, "Profile")
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", settingsFile, err)
	}
	root := d.root
	var edits []edit
	set := func(n *node, text string) {
		if n.empty {
			edits = append(edits, edit{n.start, n.end, "<" + n.name.Local + ">" + text + "</" + n.name.Local + ">"})
		} else {
			edits = append(edits, edit{n.open, n.close, text})
		}
	}
	if u := root.first("UseUDPServer"); u != nil {
		set(u, "True")
	} else {
		edits = append(edits, addChildren(b, root, []string{"  <UseUDPServer>True</UseUDPServer>"}, d.eol()))
	}
	if e := root.first("UDPServerPort"); e != nil {
		if _, ok := udpPortText(e.text); !ok {
			set(e, strconv.Itoa(DefaultUDPPort))
		}
	}
	if e := root.first("UDPServerListenAddress"); e != nil && !usableAddress(e.text) {
		set(e, defaultAddress)
	}
	return apply(b, edits), nil
}

// change is one file an install writes, for one step or more.
type change struct {
	steps []string
	path  string
	name  string // as the player sees it, relative to the data folder
	old   []byte // nil when the file is new
	new   []byte
	check func(d *doc) error
}

// Result is what an install wrote.
type Result struct {
	Dir      string   // the data folder
	Backup   string   // the folder with the copies; "" when none was made
	Wrote    []string // the steps written, in order; they stay written when a later one fails
	Restored bool     // the file that failed was put back from the backup
}

// Did says what the install did, for the log.
func (r Result) Did() string {
	if len(r.Wrote) == 0 {
		return "nothing was left to write in " + r.Dir
	}
	var did []string
	for _, s := range r.Wrote {
		switch s {
		case StepProfile:
			did = append(did, fmt.Sprintf("wrote the profile %q", ProfileName))
		case StepRule:
			did = append(did, "added the Auto Profiles rule for Elite")
		case StepListener:
			did = append(did, "turned game mod support on")
		case StepUDPServer:
			did = append(did, "turned DS4Windows' UDP server on")
		}
	}
	list := did[len(did)-1]
	if len(did) > 1 {
		list = strings.Join(did[:len(did)-1], ", ") + " and " + list
	}
	return fmt.Sprintf("%s, in %s; copies of the old files are in %s", list, r.Dir, r.Backup)
}

// notClosed: DS4Windows runs, or one of its files changed, before
// anything was written; the install waits again.
type notClosed struct{ why string }

func (e notClosed) Error() string { return "DS4Windows is not closed (" + e.why + ")" }

// InterruptedError: DS4Windows started, or one of its files changed,
// after EDSense had written some of them. The rest are left as they are.
type InterruptedError struct {
	Why   string
	Wrote []string // the steps written before it
}

func (e *InterruptedError) Error() string {
	return fmt.Sprintf("DS4Windows started while EDSense was writing its settings (%s), so EDSense stopped after: %s",
		e.Why, strings.Join(e.Wrote, ", "))
}

// ChangedError: DS4Windows' files changed while the install waited, and
// it would now write a step the player was not asked about, or move the
// UDP server from an address the question did not name. Nothing is
// written.
type ChangedError struct {
	Step  string
	Moves string // the UDP server's address it would now replace
}

func (e *ChangedError) Error() string {
	if e.Moves != "" {
		return fmt.Sprintf("DS4Windows' settings changed while EDSense waited: its UDP server listens on %s now, "+
			"which the question did not name; nothing was written", e.Moves)
	}
	return fmt.Sprintf("DS4Windows' settings changed while EDSense waited, and it would now also write %s, "+
		"which the question did not name; nothing was written", StepFile(e.Step))
}

// writer does one install.
type writer struct {
	backups string                // the folder the backup folders go in
	closed  func() (bool, string) // DS4Windows is closed; else why not
	now     time.Time
	put     func(path string, b []byte) error
	moves   *string // the UDP server address the player was told the install replaces; nil: any
}

// install writes what the plan of in has to do, as Inspect finds it now.
// want are the steps the player agreed to (nil: any): a plan that needs
// another one now writes nothing. Every file is made and checked before
// the first one is written, and copies of all of them are kept before it.
// DS4Windows must be closed right before each write. A file that does not
// read back as written is put back from the copy.
func (w writer) install(in Input, reset bool, want []string) (Result, error) {
	res := Result{Dir: in.Dir}
	p, f := inspect(in)
	if p.State == PlanBlocked {
		return res, &BlockedError{p}
	}
	todo := p.Todo(reset)
	for _, id := range todo {
		if want != nil && !slices.Contains(want, id) {
			return res, &ChangedError{Step: id}
		}
	}
	// DS4Windows writes its UDP server's address when it exits, which may
	// be after the question
	if w.moves != nil && slices.Contains(todo, StepUDPServer) && p.UDPMoves != "" && p.UDPMoves != *w.moves {
		return res, &ChangedError{Step: StepUDPServer, Moves: p.UDPMoves}
	}
	changes, err := makeChanges(in.Dir, f, todo)
	if err != nil {
		return res, err
	}
	if len(changes) == 0 {
		return res, nil
	}
	if ok, why := w.closed(); !ok {
		return res, notClosed{why}
	}
	backup, err := w.backup(f)
	if err != nil {
		return res, fmt.Errorf("the copies could not be made, so nothing was written: %s", plainErr(err))
	}
	res.Backup = backup
	for _, c := range changes {
		why := ""
		if ok, not := w.closed(); !ok {
			why = not
		} else if cur, err := os.ReadFile(c.path); !sameFile(cur, err, c.old) {
			why = c.name + " changed"
		}
		if why != "" {
			if len(res.Wrote) == 0 {
				return res, notClosed{why}
			}
			return res, &InterruptedError{Why: why, Wrote: slices.Clone(res.Wrote)}
		}
		if err := w.put(c.path, c.new); err != nil {
			// put renames last, so the file is still the old one
			return res, fmt.Errorf("%s could not be written (%s), so it was left as it was", c.name, plainErr(err))
		}
		cur, err := os.ReadFile(c.path)
		if err == nil && !bytes.Equal(cur, c.new) {
			err = errors.New("it reads back different")
		}
		if err == nil {
			err = readsBack(cur, c.check)
		}
		if err != nil {
			if rerr := w.restore(c); rerr != nil {
				return res, fmt.Errorf("%s was not written right (%s), and could not be put back (%s): its copy is in the backup folder",
					c.name, plainErr(err), plainErr(rerr))
			}
			res.Restored = true
			return res, fmt.Errorf("%s was not written right (%s), so it was put back from its copy", c.name, plainErr(err))
		}
		res.Wrote = append(res.Wrote, c.steps...)
	}
	return res, nil
}

// makeChanges makes the files the steps in todo write in dir, from the
// files Inspect read, each checked as DS4Windows reads it. Steps on one
// file make one change, written once and read back for all of them.
func makeChanges(dir string, f *files, todo []string) ([]change, error) {
	var changes []change
	for _, id := range todo {
		c := change{steps: []string{id}}
		same := -1 // the change of the same file, which this step adds to
		var err error
		switch id {
		case StepProfile:
			c.path, c.name, c.old = filepath.Join(dir, profilesDir, ProfileName+".xml"), StepFile(id), f.ours
			if f.oursErr != nil {
				return nil, fmt.Errorf("%s could not be read: %s", ourFile, plainErr(f.oursErr))
			}
			c.new, c.check = ProfileFile(f.appVersion), validOurProfile
		case StepRule:
			c.path, c.name, c.old = filepath.Join(dir, autoProfilesFile), StepFile(id), f.auto
			c.new, err = SpliceRule(f.auto)
			c.check = func(d *doc) error { return hasOurRule(d, f.programs+1) }
		case StepListener, StepUDPServer:
			set, check := SetListener, checkListener
			if id == StepUDPServer {
				set, check = SetUDPServer, checkUDPServer
			}
			c.path, c.name, c.old = filepath.Join(dir, settingsFile), StepFile(id), f.settings
			c.new, err = set(f.settings)
			c.check = check
			if same = slices.IndexFunc(changes, func(o change) bool { return o.path == c.path }); same >= 0 {
				prev := changes[same]
				c.steps = append(slices.Clone(prev.steps), id)
				c.new, err = set(prev.new)
				c.check = func(d *doc) error {
					if err := prev.check(d); err != nil {
						return err
					}
					return check(d)
				}
			}
		}
		if err == nil {
			err = readsBack(c.new, c.check)
		}
		if err != nil {
			return nil, fmt.Errorf("EDSense could not make %s: %w", c.name, err)
		}
		if same >= 0 {
			changes[same] = c
		} else {
			changes = append(changes, c)
		}
	}
	return changes, nil
}

// sameFile: the file read now is the one Inspect read; old nil means it
// was missing.
func sameFile(cur []byte, err error, old []byte) bool {
	if old == nil {
		return errors.Is(err, fs.ErrNotExist)
	}
	return err == nil && bytes.Equal(cur, old)
}

// readsBack: b reads strictly and passes check.
func readsBack(b []byte, check func(*doc) error) error {
	d, err := scan(b)
	if err == nil {
		err = check(d)
	}
	return err
}

// hasOurRule: the rules DS4Windows reads are programs in all, the last
// one EDSense's.
func hasOurRule(d *doc, programs int) error {
	if err := validAutoProfiles(d); err != nil {
		return err
	}
	var last *node
	n := 0
	for _, k := range d.root.kids {
		if k.name.Space == "" && k.name.Local == "Program" {
			last = k
			n++
		}
	}
	if n != programs {
		return fmt.Errorf("%d rules where %d were meant", n, programs)
	}
	path, _ := last.attrValue("path")
	device, _ := last.attrValue("device")
	all, _ := last.attrValue("applyToAllControllers")
	c1 := last.first("Controller1")
	if path != RulePath || device != "DualSense" || all != "true" || c1 == nil || c1.text != ProfileName {
		return errors.New("EDSense's rule is not the last one")
	}
	return nil
}

// checkListener: Profiles.xml reads, and game mod support is on where
// DS4Windows takes it.
func checkListener(d *doc) error {
	if err := validSettings(d); err != nil {
		return err
	}
	if !listenerOn(d.root) {
		return errors.New("game mod support is not on")
	}
	return nil
}

// backup copies the files Inspect read into a new folder named by the
// time, under w.backups. On any error it takes back what it made.
func (w writer) backup(f *files) (string, error) {
	if w.backups == "" {
		return "", errors.New("no folder for the copies")
	}
	if err := os.MkdirAll(w.backups, 0o755); err != nil {
		return "", err
	}
	stamp := w.now.Format("20060102-150405")
	folder := ""
	for i := 1; folder == ""; i++ {
		try := filepath.Join(w.backups, stamp)
		if i > 1 {
			try += fmt.Sprintf("-%d", i)
		}
		switch err := os.Mkdir(try, 0o755); {
		case err == nil:
			folder = try
		case !errors.Is(err, fs.ErrExist) || i >= 100:
			return "", err
		}
	}
	copies := []struct {
		name string
		b    []byte
	}{{settingsFile, f.settings}, {autoProfilesFile, f.auto}}
	if f.ours != nil {
		copies = append(copies, struct {
			name string
			b    []byte
		}{ProfileName + ".xml", f.ours})
	}
	var made []string
	for _, c := range copies {
		path := filepath.Join(folder, c.name)
		made = append(made, path)
		err := writeSynced(path, c.b)
		if err == nil {
			var back []byte
			if back, err = os.ReadFile(path); err == nil && !bytes.Equal(back, c.b) {
				err = fmt.Errorf("the copy of %s reads back different", c.name)
			}
		}
		if err != nil {
			for _, m := range made {
				os.Remove(m)
			}
			os.Remove(folder)
			return "", err
		}
	}
	return folder, nil
}

// writeSynced writes b as a new file at path and flushes it to the disk,
// so the copy is there for sure before the file it copies is replaced.
func writeSynced(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// restore puts back the file a change wrote: the old bytes, or no file
// when it was new.
func (w writer) restore(c change) error {
	if c.old == nil {
		if err := os.Remove(c.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := put(c.path, c.old); err != nil {
		return err
	}
	if cur, err := os.ReadFile(c.path); err != nil || !bytes.Equal(cur, c.old) {
		return errors.New("it reads back different")
	}
	return nil
}

// put writes b to path through a new file in the same folder, renamed
// over the old one, so the file is either the old one or the new one.
func put(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "edsense-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	for i := 0; err == nil; i++ {
		// a virus scanner may hold the file for a moment
		if err = os.Rename(tmp, path); err == nil || i == 4 {
			break
		}
		time.Sleep(50 * time.Millisecond)
		err = nil
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// plainErr is an error without the folders of its paths, for the window.
func plainErr(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + " " + filepath.Base(pe.Path) + ": " + pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Op + " " + filepath.Base(le.Old) + " " + filepath.Base(le.New) + ": " + le.Err.Error()
	}
	return err.Error()
}

// Job states.
const (
	JobWaiting     = "waiting"     // for DS4Windows to be closed
	JobDone        = "done"        // written
	JobFailed      = "failed"      // not written, or put back from the copies
	JobInterrupted = "interrupted" // DS4Windows started while EDSense wrote: the steps before are written, the rest not
)

// Job is the install asked for last.
type Job struct {
	State   string // "" when none was asked for
	Dir     string // the data folder it writes
	Reset   bool
	Steps   []string // the steps asked for: the install writes no other
	Moves   string   // the UDP server address it replaces with 127.0.0.1, as the question named it; "": none
	Writing bool     // it writes now, so it can no longer be cancelled
	Result  Result
	Err     string // why it failed or stopped, without full paths
}

// ErrBusy: an install is writing.
var ErrBusy = errors.New("EDSense is writing DS4Windows' settings")

// ErrNothing: the plan has nothing to write.
var ErrNothing = errors.New("there is nothing to write")

// InstallerOptions set up an Installer.
type InstallerOptions struct {
	Backups string                             // the folder the backup folders go in: <EDSense data>\ds4windows_backups
	Closed  func(exeDir string) (bool, string) // DS4Windows is closed, else why not; nil: Closed
	Now     func() time.Time                   // nil: time.Now
	Hold    time.Duration                      // how long DS4Windows must stay closed before the first write; 0: 2 s
}

// Installer runs one install at a time: it waits until DS4Windows has been
// closed for a while (its exit save has ended), then writes. Step drives
// it, every second or so; the other methods may be called from any
// goroutine.
type Installer struct {
	o   InstallerOptions
	put func(path string, b []byte) error

	mu      sync.Mutex
	job     Job
	in      Input
	gen     int  // counts requests and cancels
	writing bool // an install writes, without mu
	since   time.Time
	told    bool // the waiting line is logged
}

// NewInstaller makes an Installer.
func NewInstaller(o InstallerOptions) *Installer {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Hold <= 0 {
		o.Hold = 2 * time.Second
	}
	if o.Closed == nil {
		o.Closed = Closed
	}
	return &Installer{o: o, put: put}
}

// ErrChanged: the steps to write now are not the ones the player was
// shown.
var ErrChanged = errors.New("what EDSense would write in DS4Windows' settings changed since the question was shown")

// Request asks for an install into in.Dir, or with reset for EDSense's
// profile to be written again. It waits for DS4Windows to be closed, and
// then writes none but the steps to do now. It refuses a blocked plan or
// one with nothing to write, and while an install writes. A request while
// one waits replaces it.
func (i *Installer) Request(in Input, reset bool) error { return i.RequestSteps(in, reset, nil) }

// RequestSteps is Request for the steps the player was shown: it refuses
// with ErrChanged when the steps to do now are others. nil takes the
// steps to do now.
func (i *Installer) RequestSteps(in Input, reset bool, shown []string) error {
	p := Inspect(in)
	if p.State == PlanBlocked {
		return &BlockedError{p}
	}
	todo := p.Todo(reset)
	if len(todo) == 0 {
		return ErrNothing
	}
	if shown != nil && !slices.Equal(shown, todo) {
		return ErrChanged
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.writing {
		return ErrBusy
	}
	i.gen++
	i.in = in
	i.job = Job{State: JobWaiting, Dir: in.Dir, Reset: reset, Steps: todo, Moves: p.UDPMoves}
	i.since, i.told = time.Time{}, false
	return nil
}

// Cancel ends an install that waits; false when none waits.
func (i *Installer) Cancel() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.job.State != JobWaiting || i.writing {
		return false
	}
	i.gen++
	i.job = Job{}
	return true
}

// Job is the install asked for last.
func (i *Installer) Job() Job {
	i.mu.Lock()
	defer i.mu.Unlock()
	j := i.job
	j.Steps, j.Result.Wrote = slices.Clone(j.Steps), slices.Clone(j.Result.Wrote)
	j.Writing = i.writing
	return j
}

// Step writes once DS4Windows has been closed for the hold time, checking
// again right before each write.
func (i *Installer) Step() {
	i.mu.Lock()
	if i.job.State != JobWaiting || i.writing {
		i.mu.Unlock()
		return
	}
	gen, exeDir := i.gen, i.in.ExeDir
	i.mu.Unlock()

	ok, _ := i.o.Closed(exeDir)
	now := i.o.Now()

	i.mu.Lock()
	if gen != i.gen {
		i.mu.Unlock()
		return
	}
	if !ok {
		i.since = time.Time{}
		i.waitLine()
		i.mu.Unlock()
		return
	}
	if i.since.IsZero() {
		i.since = now
	}
	if now.Sub(i.since) < i.o.Hold {
		i.mu.Unlock()
		return
	}
	i.writing = true
	in, reset, want, moves := i.in, i.job.Reset, slices.Clone(i.job.Steps), i.job.Moves
	i.mu.Unlock()

	closed := func() (bool, string) { return i.o.Closed(in.ExeDir) }
	w := writer{backups: i.o.Backups, closed: closed, now: now, put: i.put, moves: &moves}
	res, err := w.safeInstall(in, reset, want)

	i.mu.Lock()
	defer i.mu.Unlock()
	i.writing = false
	var nc notClosed
	var stop *InterruptedError
	switch {
	case errors.As(err, &nc):
		i.since = time.Time{} // wait again
		i.waitLine()
		return
	case err == nil:
		i.job.State = JobDone
		log.Printf("DS4Windows profile: %s", res.Did())
	case errors.As(err, &stop):
		i.job.State, i.job.Err = JobInterrupted, err.Error()
		log.Printf("DS4Windows profile not written: %v", err)
	default:
		i.job.State, i.job.Err = JobFailed, err.Error()
		log.Printf("DS4Windows profile not written: %v", err)
	}
	i.job.Result = res
}

// safeInstall is install, with a panic made an error: the job fails, and
// the Installer and EDSense go on.
func (w writer) safeInstall(in Input, reset bool, want []string) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res.Dir, err = in.Dir, fmt.Errorf("EDSense hit a bug while writing: %v", r)
		}
	}()
	return w.install(in, reset, want)
}

// waitLine logs once per install that it waits.
func (i *Installer) waitLine() {
	if !i.told {
		log.Print("DS4Windows profile: waiting for DS4Windows to be closed")
		i.told = true
	}
}
