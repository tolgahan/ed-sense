package dualsense

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}

var (
	hidDLL            = windows.NewLazySystemDLL("hid.dll")
	procGetAttributes = hidDLL.NewProc("HidD_GetAttributes")
	procGetProduct    = hidDLL.NewProc("HidD_GetProductString")
	procGetPreparsed  = hidDLL.NewProc("HidD_GetPreparsedData")
	procFreePreparsed = hidDLL.NewProc("HidD_FreePreparsedData")
	procGetCaps       = hidDLL.NewProc("HidP_GetCaps")
	cfgmgr            = windows.NewLazySystemDLL("cfgmgr32.dll")
	procLocateDevNode = cfgmgr.NewProc("CM_Locate_DevNodeW")
	procGetParent     = cfgmgr.NewProc("CM_Get_Parent")
	procGetDeviceID   = cfgmgr.NewProc("CM_Get_Device_IDW")
)

const sonyVendorID = 0x054C

// HIDD_ATTRIBUTES
type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

// HIDP_CAPS
type hidpCaps struct {
	Usage                   uint16
	UsagePage               uint16
	InputReportByteLength   uint16
	OutputReportByteLength  uint16
	FeatureReportByteLength uint16
	Reserved                [17]uint16
	Counts                  [10]uint16
}

func (d HIDDevice) isDualSense() bool { return d.ProductID == 0x0CE6 || d.ProductID == 0x0DF2 }

// ListHID enumerates the Sony HID interfaces this process can see. Devices
// hidden by HidHide are not listed.
func ListHID() []HIDDevice {
	paths, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil
	}
	var out []HIDDevice
	for _, path := range paths {
		if d, ok := describeHID(path); ok {
			out = append(out, d)
		}
	}
	return out
}

func describeHID(path string) (HIDDevice, bool) {
	if path == "" {
		return HIDDevice{}, false
	}
	h, err := openHID(path, 0)
	if err != nil {
		return HIDDevice{}, false
	}
	defer windows.CloseHandle(h)
	attr := hiddAttributes{Size: uint32(unsafe.Sizeof(hiddAttributes{}))}
	if r, _, _ := procGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr))); r == 0 || attr.VendorID != sonyVendorID {
		return HIDDevice{}, false
	}
	d := HIDDevice{Path: path, ProductID: attr.ProductID, Product: hidString(h, procGetProduct)}
	d.InLen, d.OutLen = reportLengths(h)
	d.Parents = deviceParents(instanceID(path), 8)
	d.Kind = classify(path, d.Parents)
	return d, true
}

func reportLengths(h windows.Handle) (in, out int) {
	var preparsed uintptr
	if r, _, _ := procGetPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&preparsed))); r == 0 {
		return 0, 0
	}
	defer procFreePreparsed.Call(preparsed)
	var caps hidpCaps
	procGetCaps.Call(preparsed, uintptr(unsafe.Pointer(&caps)))
	return int(caps.InputReportByteLength), int(caps.OutputReportByteLength)
}

func hidString(h windows.Handle, proc *windows.LazyProc) string {
	buf := make([]uint16, 256)
	if r, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2)); r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func openHID(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

// deviceParents walks up the device tree from a device instance ID.
func deviceParents(id string, max int) []string {
	p, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return nil
	}
	var node uint32
	if r, _, _ := procLocateDevNode.Call(uintptr(unsafe.Pointer(&node)), uintptr(unsafe.Pointer(p)), 0); r != 0 {
		return nil
	}
	var out []string
	for range max {
		var parent uint32
		if r, _, _ := procGetParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(node), 0); r != 0 {
			break
		}
		buf := make([]uint16, 512)
		if r, _, _ := procGetDeviceID.Call(uintptr(parent), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0); r != 0 {
			break
		}
		out = append(out, windows.UTF16ToString(buf))
		node = parent
	}
	return out
}

// pickVirtual returns DSX's virtual DualSense (DSX's DualSense emulation).
func pickVirtual(devices []HIDDevice) (HIDDevice, bool) {
	for _, kind := range []string{Virtual, Unknown} {
		for _, d := range devices {
			if d.isDualSense() && d.Kind == kind && d.InLen > 0 && d.OutLen > 0 {
				return d, true
			}
		}
	}
	return HIDDevice{}, false
}

// Link keeps DSX's virtual DualSense open: it reads its input and writes
// rumble.
type Link struct {
	mu       sync.Mutex
	dev      HIDDevice
	read     windows.Handle
	write    windows.Handle
	open     bool
	state    State
	pressed  Button
	report   func(State, time.Time) // set by OnReport
	lastScan time.Time
	warned   bool

	reports    chan []byte
	lastLeft   uint8
	lastRight  uint8
	lastReport time.Time
}

func NewLink() *Link {
	l := &Link{reports: make(chan []byte, 1)}
	go l.writeLoop()
	return l
}

func (l *Link) Available() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open
}

// Maintain opens the virtual pad when it is not open, at most every 3 s.
func (l *Link) Maintain() {
	l.mu.Lock()
	if l.open || time.Since(l.lastScan) < 3*time.Second {
		l.mu.Unlock()
		return
	}
	l.lastScan = time.Now()
	l.mu.Unlock()

	d, ok := pickVirtual(ListHID())
	if !ok {
		l.warnOnce("Haptics: no virtual DualSense found (needs DSX's DualSense emulation)")
		return
	}
	read, err := openHID(d.Path, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		log.Printf("Haptics: cannot open the virtual DualSense: %v", err)
		return
	}
	write, err := openHID(d.Path, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		windows.CloseHandle(read)
		log.Printf("Haptics: cannot open the virtual DualSense for writing: %v", err)
		return
	}
	l.mu.Lock()
	l.dev, l.read, l.write, l.open, l.warned, l.state = d, read, write, true, false, State{}
	l.mu.Unlock()
	log.Printf("Haptics: using %s", d)
	go l.readLoop(read, d.InLen)
}

func (l *Link) warnOnce(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.warned {
		log.Print(msg)
		l.warned = true
	}
}

func (l *Link) readLoop(h windows.Handle, size int) {
	buf := make([]byte, max(size, 64))
	var last Button
	for {
		var n uint32
		if err := windows.ReadFile(h, buf, &n, nil); err != nil {
			l.lost(h, err)
			return
		}
		at := time.Now()
		st, ok := ParseInputReport(buf[:n])
		if !ok {
			continue
		}
		l.mu.Lock()
		l.pressed |= st.Buttons &^ last
		l.state = st
		f := l.report
		l.mu.Unlock()
		last = st.Buttons
		if f != nil {
			f(st, at)
		}
	}
}

// OnReport calls f with every input report as it arrives, and the time it
// was read. OnReport(nil) stops it, though a call already under way may
// finish after OnReport returns. f runs on the goroutine that reads the
// controller: it must return quickly, or reports queue up in the driver,
// and it must not call the Link.
func (l *Link) OnReport(f func(State, time.Time)) {
	l.mu.Lock()
	l.report = f
	l.mu.Unlock()
}

func (l *Link) lost(h windows.Handle, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.read != h {
		return
	}
	log.Printf("Haptics: virtual DualSense lost (%v)", err)
	windows.CloseHandle(l.read)
	windows.CloseHandle(l.write)
	l.open, l.state, l.lastScan = false, State{}, time.Now()
}

// State returns the latest input, with the buttons pressed since the last call.
func (l *Link) State() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.state
	st.Pressed, l.pressed = l.pressed, 0
	return st
}

// SetRumble sets the motor levels. A non-zero level is re-sent every 250 ms,
// or DSX lets it fade.
func (l *Link) SetRumble(left, right uint8) {
	l.mu.Lock()
	if !l.open {
		l.mu.Unlock()
		return
	}
	same := left == l.lastLeft && right == l.lastRight
	if same && (left == 0 && right == 0 || time.Since(l.lastReport) < 250*time.Millisecond) {
		l.mu.Unlock()
		return
	}
	l.lastLeft, l.lastRight, l.lastReport = left, right, time.Now()
	report := RumbleReport(l.dev.OutLen, left, right)
	l.mu.Unlock()
	// replace a report still waiting to be written with the newest one
	select {
	case <-l.reports:
	default:
	}
	l.reports <- report
}

func (l *Link) writeLoop() {
	for report := range l.reports {
		l.mu.Lock()
		h, open := l.write, l.open
		l.mu.Unlock()
		if !open {
			continue
		}
		var n uint32
		if err := windows.WriteFile(h, report, &n, nil); err != nil && !errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
			l.warnOnce(fmt.Sprintf("Haptics: rumble write failed: %v", err))
		}
	}
}

func (l *Link) Close() {
	l.SetRumble(0, 0)
	time.Sleep(50 * time.Millisecond)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open {
		windows.CancelIoEx(l.read, nil)
		windows.CloseHandle(l.read)
		windows.CloseHandle(l.write)
		l.open = false
	}
}
