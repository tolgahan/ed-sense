package ds4w

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A fake DS4Windows window for Query, under names of its own, so no
// DS4Windows that runs on the machine is ever asked.

var (
	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procUnregisterClass  = user32.NewProc("UnregisterClassW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procGetMessage       = user32.NewProc("GetMessageW")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	eventModifyState = 0x0002
)

type wndClassEx struct {
	Size, Style        uint32
	WndProc            uintptr
	ClsExtra, WndExtra int32
	Instance, Icon     uintptr
	Cursor, Background uintptr
	MenuName           *uint16
	ClassName          *uint16
	IconSm             uintptr
}

type winMsg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             [2]int32
	Private        uint32
}

// fakeDS4Windows answers questions the way DS4Windows does: into the
// answer block, without an end mark, then the ready event.
type fakeDS4Windows struct {
	names  Names
	class  string
	answer func(q string) (string, bool) // false: no answer, as its portable lab
	keep   bool                          // keep the answer block open, as a stale asker would

	mu    sync.Mutex
	asked []string
	kept  []windows.Handle
	hwnd  uintptr
	done  chan struct{}
	block windows.Handle
}

var (
	current     *fakeDS4Windows
	wndProcOnce sync.Once
	wndProc     uintptr
)

func startFake(t *testing.T, answer func(string) (string, bool)) *fakeDS4Windows {
	t.Helper()
	tag := fmt.Sprintf("EDSenseTest_%d_%d", os.Getpid(), time.Now().UnixNano())
	f := &fakeDS4Windows{
		names: Names{ClassBlock: tag + "_class", ResultBlock: tag + "_result", Mutex: tag + "_mutex", Ready: tag + "_ready", Title: tag + " window"},
		class: tag + "Class", answer: answer, done: make(chan struct{}),
	}
	current = f
	wndProcOnce.Do(func() { wndProc = windows.NewCallback(fakeWndProc) })
	ready := make(chan error)
	go f.run(ready)
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	// the class name, published as DS4Windows does
	block, err := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, classBlockSize, wide(f.names.ClassBlock))
	if block == 0 {
		t.Fatal(err)
	}
	f.block = block
	view, err := windows.MapViewOfFile(block, windows.FILE_MAP_WRITE, 0, 0, classBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	name := []byte(f.class)
	moveMemory(view, uintptr(unsafe.Pointer(&name[0])), len(name))
	windows.UnmapViewOfFile(view)
	t.Cleanup(f.stop)
	return f
}

func (f *fakeDS4Windows) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	inst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{WndProc: wndProc, Instance: inst, ClassName: wide(f.class)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		ready <- fmt.Errorf("RegisterClassEx: %v", err)
		return
	}
	defer procUnregisterClass.Call(uintptr(unsafe.Pointer(wide(f.class))), inst)
	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(wide(f.class))), uintptr(unsafe.Pointer(wide(f.names.Title))), 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		ready <- fmt.Errorf("CreateWindowEx: %v", err)
		return
	}
	f.mu.Lock()
	f.hwnd = hwnd
	f.mu.Unlock()
	ready <- nil
	var m winMsg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	close(f.done)
}

func (f *fakeDS4Windows) stop() {
	f.mu.Lock()
	hwnd := f.hwnd
	for _, h := range f.kept {
		windows.CloseHandle(h)
	}
	f.mu.Unlock()
	procPostMessage.Call(hwnd, wmClose, 0, 0)
	<-f.done
	windows.CloseHandle(f.block)
}

func fakeWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	f := current
	switch msg {
	case wmCopyData:
		var cd copyData
		moveMemory(uintptr(unsafe.Pointer(&cd)), lParam, int(unsafe.Sizeof(cd)))
		q := make([]byte, cd.Size)
		if len(q) > 0 {
			moveMemory(uintptr(unsafe.Pointer(&q[0])), cd.Ptr, len(q))
		}
		f.mu.Lock()
		f.asked = append(f.asked, string(q))
		f.mu.Unlock()
		if a, ok := f.answer(string(q)); ok {
			f.write(a)
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// write answers as DS4Windows does: into the block the asker made, with
// no end mark, then the event.
func (f *fakeDS4Windows) write(answer string) {
	h, _, _ := procOpenFileMapping.Call(windows.FILE_MAP_WRITE, 0, uintptr(unsafe.Pointer(wide(f.names.ResultBlock))))
	if h != 0 {
		if view, err := windows.MapViewOfFile(windows.Handle(h), windows.FILE_MAP_WRITE, 0, 0, resultBlockSize); err == nil {
			if b := []byte(answer); len(b) > 0 {
				moveMemory(view, uintptr(unsafe.Pointer(&b[0])), min(len(b), resultBlockSize))
			}
			windows.UnmapViewOfFile(view)
		}
		if f.keep {
			f.mu.Lock()
			f.kept = append(f.kept, windows.Handle(h))
			f.mu.Unlock()
		} else {
			windows.CloseHandle(windows.Handle(h))
		}
	}
	if ev, err := windows.OpenEvent(eventModifyState, false, wide(f.names.Ready)); err == nil {
		windows.SetEvent(ev)
		windows.CloseHandle(ev)
	}
}

// TestQuery: EDSense asks DS4Windows' window for a controller's profile
// and reads the answer; a longer answer left in the block from before
// does not show through.
func TestQuery(t *testing.T) {
	f := startFake(t, func(q string) (string, bool) {
		switch q {
		case "Query.2.ProfileName":
			return "Elite ?stanbul", true
		case "Query.1.OutContType":
			return "ViiperDualSense", true
		case "Query.1.ProfileName":
			return "A much longer profile name than the next", true
		case "Query.3.ProfileName":
			return "Short", true
		}
		return "", true
	})
	f.keep = true
	for _, c := range []struct {
		slot       int
		prop, want string
	}{
		{1, PropProfile, "Elite ?stanbul"},
		{0, PropOutput, "ViiperDualSense"},
		{0, PropProfile, "A much longer profile name than the next"},
		{2, PropProfile, "Short"},
	} {
		got, err := Query(f.names, c.slot, c.prop)
		if err != nil || got != c.want {
			t.Errorf("slot %d %s: %q %v, want %q", c.slot, c.prop, got, err, c.want)
		}
	}
	f.mu.Lock()
	asked := strings.Join(f.asked, ",")
	f.mu.Unlock()
	if asked != "Query.2.ProfileName,Query.1.OutContType,Query.1.ProfileName,Query.3.ProfileName" {
		t.Errorf("asked %s", asked)
	}
	if _, err := Query(f.names, 8, PropProfile); err == nil {
		t.Error("slot 9 asked")
	}
}

// TestQueryNoAnswer: DS4Windows' portable lab takes the message and
// answers nothing; that ends in an error within about a second.
func TestQueryNoAnswer(t *testing.T) {
	f := startFake(t, func(string) (string, bool) { return "", false })
	start := time.Now()
	if got, err := Query(f.names, 0, PropProfile); err == nil {
		t.Fatalf("answered %q", got)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
}

// slowFirst answers as DS4Windows, its window busy for stall before the
// first answer.
func slowFirst(stall time.Duration) func(string) (string, bool) {
	first := true // the window's own thread only
	return func(q string) (string, bool) {
		if first {
			first = false
			time.Sleep(stall)
		}
		switch q {
		case "Query.1.OutContType":
			return "ViiperDualSense", true
		case "Query.1.ProfileName":
			return "Short", true
		}
		return "", true
	}
}

// TestQueryLate: a question that times out is still answered when
// DS4Windows catches up; that answer lands in its own block, never in the
// next question's.
func TestQueryLate(t *testing.T) {
	f := startFake(t, slowFirst(700*time.Millisecond))
	if got, err := Query(f.names, 0, PropOutput); err == nil {
		t.Fatalf("a question that timed out answered %q", got)
	}
	if got, err := Query(f.names, 0, PropProfile); err != nil || got != "Short" {
		t.Errorf("the next question: %q %v, want \"Short\"", got, err)
	}
}

// TestQueryLateAfterWait: DS4Windows answers later than the question waits
// for; the next question lets it work through that one first.
func TestQueryLateAfterWait(t *testing.T) {
	old := lateWaitMs
	lateWaitMs = 100
	defer func() { lateWaitMs = old }()
	f := startFake(t, slowFirst(900*time.Millisecond))
	if got, err := Query(f.names, 0, PropOutput); err == nil {
		t.Fatalf("a question that timed out answered %q", got)
	}
	got, err := Query(f.names, 0, PropProfile)
	if err != nil {
		got, err = Query(f.names, 0, PropProfile) // DS4Windows was still busy
	}
	if err != nil || got != "Short" {
		t.Errorf("the next question: %q %v, want \"Short\"", got, err)
	}
}

// TestWindowProcess: the program behind DS4Windows' window is found
// through the class name it publishes, whatever its exe is called.
func TestWindowProcess(t *testing.T) {
	f := startFake(t, func(string) (string, bool) { return "", true })
	exe, ok := WindowProcess(f.names)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !strings.EqualFold(filepath.Base(exe), filepath.Base(self)) {
		t.Errorf("window of %q %v, want %q", exe, ok, self)
	}
	if _, ok := WindowProcess(Names{ClassBlock: f.names.ClassBlock + "_none", Title: "none"}); ok {
		t.Error("a window without a class block")
	}
}

// TestQueryNotRunning: without DS4Windows' class block there is nobody to
// ask.
func TestQueryNotRunning(t *testing.T) {
	n := Names{ClassBlock: fmt.Sprintf("EDSenseTest_%d_none", os.Getpid()), Title: "none"}
	start := time.Now()
	if _, err := Query(n, 0, PropProfile); err == nil {
		t.Fatal("asked nobody and got an answer")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}
