// Package ds4w reads what EDSense needs to know from DS4Windows 5 (the
// VIIPER builds): where it keeps its settings, where its DSX listener
// listens, which profile a controller uses, and what that profile does
// with the gyro. DS4Windows is GPL-3; this package holds only EDSense's
// own code, written from facts about DS4Windows' files and messages.
package ds4w

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Exe is DS4Windows' process.
const Exe = "DS4Windows.exe"

// DefaultPort is where DS4Windows' DSX listener listens unless set
// otherwise, on 127.0.0.1.
const DefaultPort = 6969

const (
	settingsFile       = "Profiles.xml"
	autoProfilesFile   = "Auto Profiles.xml"
	linkedProfilesFile = "LinkedProfiles.xml"
	profilesDir        = "Profiles"
	slots              = 8 // controller slots
)

// DataDir is where DS4Windows keeps its settings: its own folder when that
// holds "Auto Profiles.xml" (a portable install), else
// %APPDATA%\DS4Windows. exeDir is "" when DS4Windows does not run.
func DataDir(exeDir, appData string) string {
	if exeDir != "" && exists(filepath.Join(exeDir, autoProfilesFile)) {
		return exeDir
	}
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, "DS4Windows")
}

// DataDirs is DataDir, and also the other folder with DS4Windows'
// settings when its own folder and %APPDATA%\DS4Windows both hold
// "Auto Profiles.xml". DS4Windows then asks at each start which one to
// use (and may keep both), so which one it uses cannot be told; also is
// "" otherwise.
func DataDirs(exeDir, appData string) (dir, also string) {
	dir = DataDir(exeDir, appData)
	if dir == "" || dir != exeDir || appData == "" {
		return dir, ""
	}
	app := filepath.Join(appData, "DS4Windows")
	if !strings.EqualFold(filepath.Clean(app), filepath.Clean(exeDir)) && exists(filepath.Join(app, autoProfilesFile)) {
		also = app
	}
	return dir, also
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Endpoint is where the DSX listener listens by these settings: its port
// and loopback address, else 127.0.0.1:6969. port, when not 0, replaces
// the port.
func (s Settings) Endpoint(port int) *net.UDPAddr {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: DefaultPort}
	ip := net.ParseIP(strings.TrimSpace(s.Address))
	if s.Port > 0 && s.Port < 65536 && ip != nil && ip.IsLoopback() {
		addr = &net.UDPAddr{IP: ip, Port: s.Port}
	}
	if port > 0 && port < 65536 {
		addr.Port = port
	}
	return addr
}

// Major is the first number of a version such as "5.0.12.0".
func Major(version string) (int, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(head)
	return n, err == nil && n > 0
}
