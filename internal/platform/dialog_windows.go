package platform

import (
	"log"

	"golang.org/x/sys/windows"
)

const idYes = 6

func messageBox(title, text string, flags uint32) int32 {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	r, _ := windows.MessageBox(0, t, c, flags)
	return r
}

func ShowInfo(title, text string)  { messageBox(title, text, windows.MB_OK|windows.MB_ICONINFORMATION) }
func ShowError(title, text string) { messageBox(title, text, windows.MB_OK|windows.MB_ICONERROR) }

func AskYesNo(title, text string) bool {
	return messageBox(title, text, windows.MB_YESNO|windows.MB_ICONQUESTION) == idYes
}

// OpenInEditor opens a text file in Notepad.
func OpenInEditor(path string) {
	verb, _ := windows.UTF16PtrFromString("open")
	exe, _ := windows.UTF16PtrFromString("notepad.exe")
	arg, _ := windows.UTF16PtrFromString(`"` + path + `"`)
	if err := windows.ShellExecute(0, verb, exe, arg, nil, windows.SW_SHOWNORMAL); err != nil {
		log.Printf("Could not open %s: %v", path, err)
	}
}
