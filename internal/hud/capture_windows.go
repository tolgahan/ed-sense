package hud

import (
	"errors"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

var (
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")

	user32             = windows.NewLazySystemDLL("user32.dll")
	procGetDC          = user32.NewProc("GetDC")
	procReleaseDC      = user32.NewProc("ReleaseDC")
	procFindWindowW    = user32.NewProc("FindWindowW")
	procGetClientRect  = user32.NewProc("GetClientRect")
	procClientToScreen = user32.NewProc("ClientToScreen")
	procIsIconic       = user32.NewProc("IsIconic")

	eliteWindowClass = windows.StringToUTF16Ptr("FrontierDevelopmentsAppWinClass")
	errNoWindow      = errors.New("game window not available")
)

type point struct{ X, Y int32 }

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// screenGrabber captures the game window with GDI. This works in windowed
// and borderless modes; exclusive fullscreen can't be captured.
type screenGrabber struct {
	mu      sync.Mutex
	hwnd    uintptr
	foundAt time.Time
	origin  point // the client area's top left on screen
}

// NewScreenGrabber captures the Elite Dangerous window.
func NewScreenGrabber() Grabber { return &screenGrabber{} }

func (g *screenGrabber) window() uintptr {
	if g.hwnd == 0 || time.Since(g.foundAt) > 5*time.Second {
		g.hwnd, _, _ = procFindWindowW.Call(uintptr(unsafe.Pointer(eliteWindowClass)), 0)
		g.foundAt = time.Now()
	}
	return g.hwnd
}

func (g *screenGrabber) Window() (int, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	h := g.window()
	if h == 0 || uintptr(windows.GetForegroundWindow()) != h {
		return 0, 0, false
	}
	if iconic, _, _ := procIsIconic.Call(h); iconic != 0 {
		return 0, 0, false
	}
	var r windows.Rect
	if ok, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return 0, 0, false
	}
	var origin point
	procClientToScreen.Call(h, uintptr(unsafe.Pointer(&origin)))
	g.origin = origin
	w, hh := int(r.Right-r.Left), int(r.Bottom-r.Top)
	return w, hh, w > 64 && hh > 64
}

func (g *screenGrabber) Grab(r vision.Rect) (*vision.Image, error) {
	g.mu.Lock()
	ox, oy := int(g.origin.X), int(g.origin.Y)
	g.mu.Unlock()
	w, h := r.W(), r.H()
	if w <= 0 || h <= 0 {
		return nil, errNoWindow
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, errNoWindow
	}
	defer procReleaseDC.Call(0, screen)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	if mem == 0 {
		return nil, errNoWindow
	}
	defer procDeleteDC.Call(mem)
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bitmap, _, _ := procCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return nil, errNoWindow
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(mem, bitmap)
	const srcCopy = 0x00CC0020
	ok, _, _ := procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(ox+r.X0), uintptr(oy+r.Y0), srcCopy)
	procSelectObject.Call(mem, old)
	if ok == 0 {
		return nil, errNoWindow
	}
	out := vision.NewImage(w, h)
	copy(out.Pix, unsafe.Slice((*byte)(bits), w*h*4))
	return out, nil
}
