package ds4w

import (
	"path/filepath"
	"strings"
)

// InstanceEvent is the named event a running DS4Windows holds, so a second
// one signals it and exits. EDSense only opens it for a moment, and never
// sets it.
const InstanceEvent = "{a52b5b20-d9ee-4f32-8518-307fa14aa0c6}"

// customExeFile, next to DS4Windows' exe, names the exe it was renamed to.
const customExeFile = "custom_exe_name.txt"

// ClosedNames are what tells that DS4Windows runs. Tests use their own.
type ClosedNames struct {
	Exe   string // its exe's name
	Event string // its single-instance event
	IPC   Names  // its window
}

// DS4WindowsClosedNames are DS4Windows' own.
var DS4WindowsClosedNames = ClosedNames{Exe: Exe, Event: InstanceEvent, IPC: DS4WindowsNames}

// Closed reports whether no DS4Windows runs, and else why it seems to:
// no process by its exe's name, or by the name in custom_exe_name.txt
// when exeDir is known; no DS4Windows window; and no single-instance
// event. Anything it cannot tell counts as running.
func Closed(exeDir string) (bool, string) { return closedWith(DS4WindowsClosedNames, exeDir) }

// customExe is the exe named in exeDir's custom_exe_name.txt, "" when
// there is none or the name is not a plain file name.
func customExe(exeDir string, read func(string) ([]byte, error)) string {
	if exeDir == "" {
		return ""
	}
	b, err := read(filepath.Join(exeDir, customExeFile))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(utf8Text(b)))
	if name == "" || strings.ContainsAny(name, `\/:*?"<>|`) || name == "." || name == ".." {
		return ""
	}
	return name + ".exe"
}
