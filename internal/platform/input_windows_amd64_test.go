package platform

import (
	"testing"
	"unsafe"
)

// The Win32 structures, against the sizes and offsets of the C headers on
// 64-bit Windows.
func TestLayout(t *testing.T) {
	var in input
	var ri rawInput
	var rid rawInputDevice
	var wc wndClassEx
	var m msg
	for _, c := range []struct {
		name      string
		got, want uintptr
	}{
		{"sizeof(INPUT)", unsafe.Sizeof(in), 40},
		{"INPUT.mi", unsafe.Offsetof(in.Mi), 8},
		{"sizeof(MOUSEINPUT)", unsafe.Sizeof(in.Mi), 32},
		{"MOUSEINPUT.dwFlags", unsafe.Offsetof(in.Mi.Flags), 12},
		{"MOUSEINPUT.dwExtraInfo", unsafe.Offsetof(in.Mi.ExtraInfo), 24},

		{"sizeof(RAWINPUTHEADER)", unsafe.Sizeof(ri.Header), 24},
		{"RAWINPUTHEADER.hDevice", unsafe.Offsetof(ri.Header.Device), 8},
		{"sizeof(RAWMOUSE)", unsafe.Sizeof(ri.Mouse), 24},
		{"RAWMOUSE.ulButtons", unsafe.Offsetof(ri.Mouse.Buttons), 4},
		{"RAWMOUSE.lLastX", unsafe.Offsetof(ri.Mouse.LastX), 12},
		{"RAWMOUSE.lLastY", unsafe.Offsetof(ri.Mouse.LastY), 16},
		{"RAWMOUSE.ulExtraInformation", unsafe.Offsetof(ri.Mouse.ExtraInfo), 20},
		{"RAWINPUT.data", unsafe.Offsetof(ri.Mouse), 24},
		{"sizeof(RAWINPUT)", unsafe.Sizeof(ri), 48},

		{"sizeof(RAWINPUTDEVICE)", unsafe.Sizeof(rid), 16},
		{"RAWINPUTDEVICE.hwndTarget", unsafe.Offsetof(rid.Target), 8},

		{"sizeof(WNDCLASSEXW)", unsafe.Sizeof(wc), 80},
		{"WNDCLASSEXW.lpfnWndProc", unsafe.Offsetof(wc.WndProc), 8},
		{"WNDCLASSEXW.hInstance", unsafe.Offsetof(wc.Instance), 24},
		{"WNDCLASSEXW.lpszClassName", unsafe.Offsetof(wc.ClassName), 64},

		{"sizeof(MSG)", unsafe.Sizeof(m), 48},
		{"MSG.message", unsafe.Offsetof(m.Message), 8},
		{"MSG.wParam", unsafe.Offsetof(m.WParam), 16},
		{"MSG.lParam", unsafe.Offsetof(m.LParam), 24},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d, want %d", c.name, c.got, c.want)
		}
	}
}
