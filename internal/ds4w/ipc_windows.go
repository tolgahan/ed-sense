package ds4w

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procOpenFileMapping    = kernel32.NewProc("OpenFileMappingW")
	procMoveMemory         = kernel32.NewProc("RtlMoveMemory")
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procFindWindow         = user32.NewProc("FindWindowW")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

const (
	wmNull          = 0x0000
	wmCopyData      = 0x004A
	smtoAbortIfHung = 0x0002
	classBlockSize  = 128
	resultBlockSize = 256
	sendTimeoutMs   = 500
	mutexWaitMs     = 500
	readyWaitMs     = 1000
)

// lateWaitMs is how long a question that timed out keeps its block for the
// late answer: Windows calls a window hung after 5 s. Tests shorten it.
var lateWaitMs uint32 = 5000

// copyData is COPYDATASTRUCT.
type copyData struct {
	Data uintptr
	Size uint32
	Ptr  uintptr
}

// Query asks the running DS4Windows for prop of the controller in slot
// (0-7), as its command line would, without starting it. It can take
// seconds (about 6 when DS4Windows is slow to answer), so never on the
// loop's goroutine. DS4Windows running as
// administrator, or its portable lab, gives no answer: an error.
func Query(n Names, slot int, prop string) (string, error) {
	if slot < 0 || slot >= slots {
		return "", fmt.Errorf("no controller slot %d", slot)
	}
	hwnd, err := findWindow(n)
	if err != nil {
		return "", err
	}
	// the mutex is owned by a thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mutex, err := windows.CreateMutex(nil, false, wide(n.Mutex))
	if mutex == 0 {
		return "", fmt.Errorf("mutex: %w", err)
	}
	defer windows.CloseHandle(mutex)
	switch ev, err := windows.WaitForSingleObject(mutex, mutexWaitMs); ev {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
	default:
		return "", fmt.Errorf("another question to DS4Windows is under way (%v)", err)
	}
	defer windows.ReleaseMutex(mutex)

	// Windows does not take back a question that timed out: DS4Windows
	// answers it when it catches up, into whatever block has the answer's
	// name then. So DS4Windows first works through any such question, while
	// no block exists, and a late answer finds none to write into.
	if r, _, err := sendTimeout(hwnd, wmNull, 0); r == 0 {
		return "", fmt.Errorf("DS4Windows is busy (%v)", err)
	}

	// a fresh block for each question: DS4Windows writes no end mark, so
	// an old longer answer would show through
	block, err := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, resultBlockSize, wide(n.ResultBlock))
	if block == 0 {
		return "", fmt.Errorf("answer block: %w", err)
	}
	defer windows.CloseHandle(block)
	view, err := windows.MapViewOfFile(block, windows.FILE_MAP_READ|windows.FILE_MAP_WRITE, 0, 0, resultBlockSize)
	if err != nil {
		return "", fmt.Errorf("answer block: %w", err)
	}
	defer windows.UnmapViewOfFile(view)
	zero := make([]byte, resultBlockSize)
	moveMemory(view, uintptr(unsafe.Pointer(&zero[0])), resultBlockSize)

	ready, err := windows.CreateEvent(nil, 0, 0, wide(n.Ready))
	if ready == 0 {
		return "", fmt.Errorf("event: %w", err)
	}
	defer windows.CloseHandle(ready)
	_ = windows.ResetEvent(ready)

	question := []byte(fmt.Sprintf("Query.%d.%s", slot+1, prop))
	cd := copyData{Size: uint32(len(question)), Ptr: uintptr(unsafe.Pointer(&question[0]))}
	r, _, err := sendTimeout(hwnd, wmCopyData, uintptr(unsafe.Pointer(&cd)))
	runtime.KeepAlive(question)
	if r == 0 {
		if errors.Is(err, windows.ERROR_TIMEOUT) {
			// DS4Windows is slow, and will still answer: keep this
			// question's block while it may, so the answer lands here
			windows.WaitForSingleObject(ready, lateWaitMs)
			return "", fmt.Errorf("DS4Windows did not answer in time (%v)", err)
		}
		return "", fmt.Errorf("DS4Windows did not take the question (%v; running as administrator?)", err)
	}
	if ev, _ := windows.WaitForSingleObject(ready, readyWaitMs); ev != windows.WAIT_OBJECT_0 {
		return "", errors.New("DS4Windows gave no answer (its portable lab answers none)")
	}
	answer := make([]byte, resultBlockSize)
	moveMemory(uintptr(unsafe.Pointer(&answer[0])), view, resultBlockSize)
	if i := bytes.IndexByte(answer, 0); i >= 0 {
		answer = answer[:i]
	}
	return string(answer), nil
}

// sendTimeout sends msg to the window, waiting at most sendTimeoutMs.
func sendTimeout(hwnd windows.HWND, msg uint32, lParam uintptr) (uintptr, uintptr, error) {
	var result uintptr
	return procSendMessageTimeout.Call(uintptr(hwnd), uintptr(msg), 0, lParam,
		smtoAbortIfHung, sendTimeoutMs, uintptr(unsafe.Pointer(&result)))
}

// WindowProcess is the exe of the program that owns DS4Windows' window,
// found through the class name DS4Windows publishes: DS4Windows itself,
// whatever its exe is called. ok is false when there is no such window;
// exe is "" when the program's path cannot be read.
func WindowProcess(n Names) (exe string, ok bool) {
	hwnd, err := findWindow(n)
	if err != nil {
		return "", false
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
		return "", true
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", true
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", true
	}
	return windows.UTF16ToString(buf[:size]), true
}

// findWindow finds DS4Windows' window by the class name it publishes.
func findWindow(n Names) (windows.HWND, error) {
	h, _, err := procOpenFileMapping.Call(windows.FILE_MAP_READ, 0, uintptr(unsafe.Pointer(wide(n.ClassBlock))))
	if h == 0 {
		return 0, fmt.Errorf("no DS4Windows window to ask (%v)", err)
	}
	defer windows.CloseHandle(windows.Handle(h))
	view, err := windows.MapViewOfFile(windows.Handle(h), windows.FILE_MAP_READ, 0, 0, classBlockSize)
	if err != nil {
		return 0, err
	}
	defer windows.UnmapViewOfFile(view)
	buf := make([]byte, classBlockSize)
	moveMemory(uintptr(unsafe.Pointer(&buf[0])), view, classBlockSize)
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	if len(buf) == 0 {
		return 0, errors.New("DS4Windows published no window class")
	}
	hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(wide(string(buf)))), uintptr(unsafe.Pointer(wide(n.Title))))
	if hwnd == 0 {
		return 0, errors.New("DS4Windows' window was not found")
	}
	return windows.HWND(hwnd), nil
}

func moveMemory(dst, src uintptr, n int) {
	procMoveMemory.Call(dst, src, uintptr(n))
}

func wide(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("")
	}
	return p
}
