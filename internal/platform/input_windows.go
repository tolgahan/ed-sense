package platform

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSendInput = user32.NewProc("SendInput")

// MOUSEINPUT
type mouseInput struct {
	Dx, Dy                 int32
	MouseData, Flags, Time uint32
	ExtraInfo              uintptr
}

// INPUT with its mouse member. Go puts Mi at offset 8 on 64-bit Windows, as
// C does, because ExtraInfo needs 8-byte alignment.
type input struct {
	Type uint32 // INPUT_MOUSE = 0
	Mi   mouseInput
}

const mouseeventfMove = 0x0001

// MoveMouse moves the mouse by relative counts, as a mouse would: raw input
// readers such as DirectInput see the counts unscaled. False: Windows
// refused (a locked screen). Input to a program running as administrator is
// dropped without an error; see InputBlocked.
func MoveMouse(dx, dy int32) bool {
	in := input{Mi: mouseInput{Dx: dx, Dy: dy, Flags: mouseeventfMove, ExtraInfo: InputTag}}
	r, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	return r == 1
}

// InputBlocked reports whether the program running exe runs at a higher
// integrity level than EDSense (as administrator), so Windows drops the
// input EDSense sends it. False when the program is not running or cannot
// be opened; true when it can be opened but its token is refused.
func InputBlocked(exe string) bool {
	own, err := tokenIntegrity(windows.GetCurrentProcessToken())
	if err != nil {
		return false
	}
	var pid uint32
	_ = eachProcess(func(name string, id uint32) bool {
		if strings.EqualFold(name, exe) {
			pid = id
		}
		return pid == 0
	})
	if pid == 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		// A program of the same user shows its token even when elevated.
		// A refusal means another account, most likely an administrator's,
		// so count it as blocked.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer tok.Close()
	rid, err := tokenIntegrity(tok)
	return err == nil && rid > own
}

// tokenIntegrity is a token's integrity level, the last sub-authority of its
// label: 0x2000 for a normal program, 0x3000 for one run as administrator.
func tokenIntegrity(t windows.Token) (uint32, error) {
	// TOKEN_MANDATORY_LABEL, then room for the SID it points to (at most
	// 68 bytes). The struct keeps the pointer typed and 8-byte aligned.
	var label struct {
		windows.Tokenmandatorylabel
		_ [68]byte
	}
	var n uint32
	if err := windows.GetTokenInformation(t, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label)), uint32(unsafe.Sizeof(label)), &n); err != nil {
		return 0, err
	}
	sid := label.Label.Sid
	if sid == nil || sid.SubAuthorityCount() == 0 {
		return 0, errors.New("the token has no integrity level")
	}
	return sid.SubAuthority(uint32(sid.SubAuthorityCount()) - 1), nil
}
