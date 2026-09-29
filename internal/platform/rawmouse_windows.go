package platform

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procUnregisterClassW        = user32.NewProc("UnregisterClassW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procGetMessageW             = user32.NewProc("GetMessageW")
	procDispatchMessageW        = user32.NewProc("DispatchMessageW")
	procPostMessageW            = user32.NewProc("PostMessageW")
	procRegisterRawInputDevices = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData         = user32.NewProc("GetRawInputData")
)

const (
	wmClose = 0x0010
	wmInput = 0x00FF

	hwndMessage = ^uintptr(2) // HWND_MESSAGE, (HWND)-3: a window that only gets messages

	usagePageGeneric = 0x01
	usageMouse       = 0x02
	ridevRemove      = 0x00000001
	ridevInputSink   = 0x00000100 // input also while another program is in front
	ridInput         = 0x10000003
	rimTypeMouse     = 0

	mouseMoveAbsolute = 0x01 // RAWMOUSE usFlags: a position, not a movement
)

// WNDCLASSEXW
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

// MSG
type msg struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// RAWINPUTDEVICE
type rawInputDevice struct {
	UsagePage uint16
	Usage     uint16
	Flags     uint32
	Target    windows.HWND
}

// RAWINPUTHEADER
type rawInputHeader struct {
	Type   uint32
	Size   uint32
	Device windows.Handle
	WParam uintptr
}

// RAWMOUSE. Buttons is the union of ulButtons with usButtonFlags and
// usButtonData; being 4-byte aligned, it leaves 2 bytes after Flags, as in C.
type rawMouse struct {
	Flags      uint16
	Buttons    uint32
	RawButtons uint32
	LastX      int32
	LastY      int32
	ExtraInfo  uint32
}

// RAWINPUT for a mouse. RAWMOUSE is the largest member of the union, so this
// is sizeof(RAWINPUT).
type rawInput struct {
	Header rawInputHeader
	Mouse  rawMouse
}

// Windows takes one Raw Input window per device class and process.
var mouseWatching atomic.Bool

// WatchMouse calls f for every relative mouse movement, with EDSense in the
// front or not, until stop closes. It runs a message-only window on its own
// locked thread and returns once that listens, or with the error that kept
// it from listening. f runs on that thread and must return quickly; it may
// still be called for a moment after stop closes. One watch at a time: a
// call while another one runs, or is still closing, fails.
func WatchMouse(stop <-chan struct{}, f func(MouseMove)) error {
	return watchRawMouse(stop, func(ri *rawInput, at time.Time) {
		if mv, ok := mouseMove(ri, at); ok {
			f(mv)
		}
	})
}

// watchRawMouse is WatchMouse with every mouse report, moving or not.
func watchRawMouse(stop <-chan struct{}, f func(*rawInput, time.Time)) error {
	if !mouseWatching.CompareAndSwap(false, true) {
		return errors.New("the mouse is already being watched")
	}
	ready := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchMouse(stop, f, ready)
	}()
	if err := <-ready; err != nil {
		<-done // cleaned up, so the caller may try again
		return err
	}
	return nil
}

func watchMouse(stop <-chan struct{}, f func(*rawInput, time.Time), ready chan<- error) {
	defer mouseWatching.Store(false)
	// A window's messages go to the thread that made it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var inst windows.Handle
	if err := windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &inst); err != nil {
		ready <- fmt.Errorf("GetModuleHandleEx: %w", err)
		return
	}
	// DefWindowProc is the window procedure: the loop below takes what it
	// needs from the messages before they are dispatched, so Windows never
	// calls into Go.
	name, _ := windows.UTF16PtrFromString("EDSenseMouseWatch")
	wc := wndClassEx{WndProc: procDefWindowProcW.Addr(), Instance: inst, ClassName: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		ready <- fmt.Errorf("RegisterClassExW: %w", err)
		return
	}
	class := uintptr(uint16(r)) // the atom, in the low word of lpClassName
	defer procUnregisterClassW.Call(class, uintptr(inst))
	r, _, err = procCreateWindowExW.Call(0, class, 0, 0, 0, 0, 0, 0, hwndMessage, 0, uintptr(inst), 0)
	if r == 0 {
		ready <- fmt.Errorf("CreateWindowExW: %w", err)
		return
	}
	hwnd := windows.HWND(r)
	defer procDestroyWindow.Call(uintptr(hwnd))
	rid := rawInputDevice{UsagePage: usagePageGeneric, Usage: usageMouse, Flags: ridevInputSink, Target: hwnd}
	if r, _, err := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid)); r == 0 {
		ready <- fmt.Errorf("RegisterRawInputDevices: %w", err)
		return
	}
	defer func() {
		// RIDEV_REMOVE fails unless the target is nil.
		rid := rawInputDevice{UsagePage: usagePageGeneric, Usage: usageMouse, Flags: ridevRemove}
		procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid))
	}()
	ready <- nil

	ended := make(chan struct{})
	closer := make(chan struct{})
	go func() {
		defer close(closer)
		select {
		case <-stop:
			procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
		case <-ended:
		}
	}()
	var m msg
	var ri rawInput
	for {
		// -1 is an error, which ends the watch too
		if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
			break
		}
		if m.Message == wmClose && m.Hwnd == hwnd {
			break
		}
		if m.Message == wmInput {
			at := time.Now()
			if readRawMouse(m.LParam, &ri) {
				f(&ri, at)
			}
		}
		// DefWindowProc frees WM_INPUT's data
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	close(ended)
	// Wait for the closer, so it cannot post to a destroyed window whose
	// handle Windows may have given to another one.
	<-closer
}

// readRawMouse reads WM_INPUT's data into ri, and reports whether it is a
// mouse's.
func readRawMouse(h uintptr, ri *rawInput) bool {
	size := uint32(unsafe.Sizeof(*ri))
	r, _, _ := procGetRawInputData.Call(h, ridInput, uintptr(unsafe.Pointer(ri)), uintptr(unsafe.Pointer(&size)), unsafe.Sizeof(ri.Header))
	// the bytes copied, or (UINT)-1
	return int32(r) >= int32(unsafe.Sizeof(*ri)) && ri.Header.Type == rimTypeMouse
}

// mouseMove is the relative movement in a mouse report, if it has one.
func mouseMove(ri *rawInput, at time.Time) (MouseMove, bool) {
	m := ri.Mouse
	if m.Flags&mouseMoveAbsolute != 0 || m.LastX == 0 && m.LastY == 0 {
		return MouseMove{}, false // a tablet or remote desktop position, or only buttons and the wheel
	}
	injected := ri.Header.Device == 0
	return MouseMove{DX: m.LastX, DY: m.LastY, Injected: injected, Ours: injected && m.ExtraInfo == InputTag, At: at}, true
}
