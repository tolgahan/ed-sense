package platform

import (
	"log"
	"os"
	"strings"

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

// OpenURL opens an https address in the default browser, never as
// administrator.
func OpenURL(address string) {
	if !strings.HasPrefix(address, "https://") {
		log.Printf("Not opening %q", address)
		return
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		// a browser started from here would run as administrator too
		if err := openAsUser(address); err != nil {
			log.Printf("Could not open %s through the desktop: %v", address, err)
			ShowInfo("EDSense", "EDSense runs as administrator, so it does not start your browser: the browser would "+
				"run as administrator too.\n\nOpen this address in your browser:\n"+address+"\n\nCtrl+C copies this message.")
		}
		return
	}
	verb, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(address)
	if err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		log.Printf("Could not open %s: %v", address, err)
	}
}

// OpenFolder shows a folder in Explorer, never as administrator: an
// elevated EDSense asks the desktop to open it. Anything but a folder is
// left alone, so nothing is run.
func OpenFolder(dir string) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		log.Printf("Not opening %s: not a folder", dir)
		return
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		if err := openAsUser(dir); err != nil {
			log.Printf("Could not open %s through the desktop: %v", dir, err)
		}
		return
	}
	verb, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(dir)
	if err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		log.Printf("Could not open %s: %v", dir, err)
	}
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
