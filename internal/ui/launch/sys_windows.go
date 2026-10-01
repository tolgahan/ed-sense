package launch

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
)

// allowForeground lets process pid bring its window to the front; the core
// may, as the player just clicked its tray icon.
func allowForeground(pid int) {
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(uint32(pid)))
}

// AllowForegroundAny lets any process come to the front until the next
// input: a second start hands the front to the running EDSense this way.
func AllowForegroundAny() {
	const asfwAny = ^uint32(0)
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(asfwAny))
}

// noInherit keeps our end of a pipe out of any other process.
func noInherit(f *os.File) {
	_ = windows.SetHandleInformation(windows.Handle(f.Fd()), windows.HANDLE_FLAG_INHERIT, 0)
}

type job windows.Handle

// jobAccounting is JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}

// Close ends every process left in the job and waits, at most a second,
// until they are gone, so the next window finds none of them.
func (j job) Close() error {
	h := windows.Handle(j)
	_ = windows.TerminateJobObject(h, 1)
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		var info jobAccounting
		if err := windows.QueryInformationJobObject(h, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil || info.ActiveProcesses == 0 {
			break
		}
	}
	return windows.CloseHandle(h)
}

// newJob puts the process in a job that ends it, and every process it
// started (WebView2's), when the job is closed, at the latest when the
// core exits.
func newJob(pid int) (io.Closer, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	defer windows.CloseHandle(p)
	if err := windows.AssignProcessToJobObject(h, p); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return job(h), nil
}

// RuntimeMissing reports that no usable WebView2 Runtime is installed, as
// the WebView2 loader would look for it: each channel, for the machine
// then the user, in the 32-bit registry view, with its DLL in place.
// found is its version, with the channel when not stable.
func RuntimeMissing() (missing bool, found string) {
	for _, ch := range runtimeChannels {
		for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			k, err := registry.OpenKey(root, runtimeKeys+ch.guid, registry.READ|registry.WOW64_32KEY)
			if err != nil {
				continue
			}
			folder, _, err := k.GetStringValue("EBWebView")
			k.Close()
			if err != nil || folder == "" || !usableRuntime(filepath.Base(folder)) {
				continue
			}
			if _, err := os.Stat(filepath.Join(folder, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")); err != nil {
				continue
			}
			found = filepath.Base(folder)
			if ch.name != "" {
				found += " " + ch.name
			}
			return false, found
		}
	}
	return true, ""
}

// openEvent is signalled by a second start of EDSense: the running one
// opens its window. A variable for the tests only.
var openEvent = `Local\EDSense-open`

// openEventSDDL lets any signed-in user's process signal it, also when
// the running EDSense is elevated and the second start is not (medium
// integrity label).
const openEventSDDL = "D:(A;;0x001F0003;;;SY)(A;;0x001F0003;;;BA)(A;;0x00100002;;;AU)S:(ML;;NW;;;ME)"

// ListenForOpen calls open whenever a second start signals, until stop.
// After stop a second start finds no EDSense to signal.
func ListenForOpen(open func()) (stop func(), err error) {
	sd, err := windows.SecurityDescriptorFromString(openEventSDDL)
	if err != nil {
		return func() {}, err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := windows.UTF16PtrFromString(openEvent)
	if err != nil {
		return func() {}, err
	}
	h, err := windows.CreateEvent(sa, 0, 0, name) // auto-reset
	if h == 0 {
		return func() {}, err
	}
	var stopped atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer windows.CloseHandle(h)
		for {
			ev, err := windows.WaitForSingleObject(h, windows.INFINITE)
			if err != nil || ev != windows.WAIT_OBJECT_0 || stopped.Load() {
				return
			}
			open()
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			stopped.Store(true)
			_ = windows.SetEvent(h) // wakes the wait, which then closes the event
			<-done
		})
	}, nil
}

// SignalRunning asks a running EDSense to open its window. It reports
// false when none runs, or one too old to listen.
func SignalRunning() bool {
	name, err := windows.UTF16PtrFromString(openEvent)
	if err != nil {
		return false
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	AllowForegroundAny()
	return windows.SetEvent(h) == nil
}
