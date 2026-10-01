package platform

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A browser that an elevated EDSense starts would run as administrator
// too. The desktop starts it instead: Explorer is not elevated, and its
// Shell object runs ShellExecute there. The way to that object: the
// desktop from IShellWindows, its top level browser, the view, and the
// view's Application.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	oleaut32             = windows.NewLazySystemDLL("oleaut32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procSysAllocString   = oleaut32.NewProc("SysAllocString")
	procVariantClear     = oleaut32.NewProc("VariantClear")

	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
)

var (
	clsidShellWindows   = windows.GUID{Data1: 0x9BA05972, Data2: 0xF6A8, Data3: 0x11CF, Data4: [8]byte{0xA4, 0x42, 0x00, 0xA0, 0xC9, 0x0A, 0x8F, 0x39}}
	iidIShellWindows    = windows.GUID{Data1: 0x85CB6900, Data2: 0x4D95, Data3: 0x11CF, Data4: [8]byte{0x96, 0x0C, 0x00, 0x80, 0xC7, 0xF4, 0xEE, 0x85}}
	iidIServiceProvider = windows.GUID{Data1: 0x6D5140C1, Data2: 0x7436, Data3: 0x11CE, Data4: [8]byte{0x80, 0x34, 0x00, 0xAA, 0x00, 0x60, 0x09, 0xFA}}
	sidSTopLevelBrowser = windows.GUID{Data1: 0x4C96BE40, Data2: 0x915C, Data3: 0x11CF, Data4: [8]byte{0x99, 0xD3, 0x00, 0xAA, 0x00, 0x4A, 0xE8, 0x37}}
	iidIShellBrowser    = windows.GUID{Data1: 0x000214E2, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDispatch        = windows.GUID{Data1: 0x00020400, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidNull             windows.GUID
)

// Vtable slots, from the Windows SDK headers.
const (
	slotQueryInterface       = 0
	slotRelease              = 2
	slotGetIDsOfNames        = 5  // IDispatch
	slotInvoke               = 6  // IDispatch
	slotQueryService         = 3  // IServiceProvider
	slotFindWindowSW         = 15 // IShellWindows
	slotQueryActiveShellView = 15 // IShellBrowser
	slotGetItemObject        = 15 // IShellView
)

const (
	swcDesktop          = 8 // SWC_DESKTOP
	swfoNeedDispatch    = 1 // SWFO_NEEDDISPATCH
	svgioBackground     = 0 // SVGIO_BACKGROUND
	localeUserDefault   = 0x400
	dispatchMethod      = 1
	dispatchPropertyGet = 2
	vtI4                = 3
	vtBSTR              = 8
	vtDispatch          = 9
	rpcEChangedMode     = 0x80010106
)

var errNoDesktop = errors.New("no desktop to start it from")

// comObject is a COM interface pointer.
type comObject struct{ vtbl *[32]uintptr }

// call calls the method in slot. Like syscall.Syscall, it keeps what its
// arguments point to alive and in place.
//
//go:uintptrescapes
func (o *comObject) call(slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObject) release() { o.call(slotRelease) }

// variant is VARIANT: the type, and the union's first word.
type variant struct {
	vt  uint16
	_   [3]uint16
	val uintptr
	_   uintptr
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func hrError(hr uintptr) error { return fmt.Errorf("HRESULT 0x%08X", uint32(hr)) }

func bstr(s string) variant {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return variant{}
	}
	b, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(p)))
	return variant{vt: vtBSTR, val: b}
}

func freeVariant(v *variant) { _, _, _ = procVariantClear.Call(uintptr(unsafe.Pointer(v))) }

// dispID is the id of an IDispatch's method or property.
func (o *comObject) dispID(name string) (int32, error) {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var id int32
	if hr := o.call(slotGetIDsOfNames, uintptr(unsafe.Pointer(&iidNull)), uintptr(unsafe.Pointer(&n)), 1,
		localeUserDefault, uintptr(unsafe.Pointer(&id))); failed(hr) {
		return 0, fmt.Errorf("%s: %w", name, hrError(hr))
	}
	return id, nil
}

// invoke calls a method, or reads a property, of an IDispatch by name.
func (o *comObject) invoke(name string, flags uint16, args ...variant) (variant, error) {
	id, err := o.dispID(name)
	if err != nil {
		return variant{}, err
	}
	// DISPPARAMS: the arguments go last first
	rev := make([]variant, len(args))
	for i, a := range args {
		rev[len(args)-1-i] = a
	}
	var params struct {
		args          *variant
		named         *int32
		nArgs, nNamed uint32
	}
	if len(rev) > 0 {
		params.args, params.nArgs = &rev[0], uint32(len(rev))
	}
	var result variant
	if hr := o.call(slotInvoke, uintptr(id), uintptr(unsafe.Pointer(&iidNull)), localeUserDefault, uintptr(flags),
		uintptr(unsafe.Pointer(&params)), uintptr(unsafe.Pointer(&result)), 0, 0); failed(hr) {
		return variant{}, fmt.Errorf("%s: %w", name, hrError(hr))
	}
	runtime.KeepAlive(rev)
	return result, nil
}

// desktopShell calls f with the desktop's Shell object, on a thread of
// its own.
func desktopShell(f func(shell *comObject) error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread ends with this goroutine, COM with it
		done <- onDesktopShell(f)
	}()
	return <-done
}

func onDesktopShell(f func(shell *comObject) error) error {
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); {
	case err == nil || err == syscall.Errno(1): // S_OK, S_FALSE
		defer windows.CoUninitialize()
	case err == syscall.Errno(rpcEChangedMode): // COM runs here already, in the other mode
	default:
		return err
	}
	var list *comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellWindows)), 0, windows.CLSCTX_LOCAL_SERVER,
		uintptr(unsafe.Pointer(&iidIShellWindows)), uintptr(unsafe.Pointer(&list))); failed(hr) || list == nil {
		return fmt.Errorf("shell windows: %w", hrError(hr))
	}
	defer list.release()

	loc := variant{vt: vtI4} // CSIDL_DESKTOP, 0
	var root variant
	var hwnd int32
	var desktop *comObject
	hr := list.call(slotFindWindowSW, uintptr(unsafe.Pointer(&loc)), uintptr(unsafe.Pointer(&root)), swcDesktop,
		uintptr(unsafe.Pointer(&hwnd)), swfoNeedDispatch, uintptr(unsafe.Pointer(&desktop)))
	if failed(hr) {
		return fmt.Errorf("desktop: %w", hrError(hr))
	}
	if desktop == nil {
		return errNoDesktop // S_FALSE: Explorer shows no desktop
	}
	defer desktop.release()

	var services *comObject
	if hr := desktop.call(slotQueryInterface, uintptr(unsafe.Pointer(&iidIServiceProvider)), uintptr(unsafe.Pointer(&services))); failed(hr) {
		return fmt.Errorf("service provider: %w", hrError(hr))
	}
	defer services.release()
	var browser *comObject
	if hr := services.call(slotQueryService, uintptr(unsafe.Pointer(&sidSTopLevelBrowser)), uintptr(unsafe.Pointer(&iidIShellBrowser)),
		uintptr(unsafe.Pointer(&browser))); failed(hr) {
		return fmt.Errorf("shell browser: %w", hrError(hr))
	}
	defer browser.release()
	var view *comObject
	if hr := browser.call(slotQueryActiveShellView, uintptr(unsafe.Pointer(&view))); failed(hr) {
		return fmt.Errorf("shell view: %w", hrError(hr))
	}
	defer view.release()
	var folder *comObject
	if hr := view.call(slotGetItemObject, svgioBackground, uintptr(unsafe.Pointer(&iidIDispatch)), uintptr(unsafe.Pointer(&folder))); failed(hr) {
		return fmt.Errorf("folder view: %w", hrError(hr))
	}
	defer folder.release()

	app, err := folder.invoke("Application", dispatchPropertyGet)
	if err != nil {
		return err
	}
	defer freeVariant(&app)
	if app.vt != vtDispatch || app.val == 0 {
		return errors.New("no Shell object")
	}
	return f(*(**comObject)(unsafe.Pointer(&app.val)))
}

// openAsUser opens address (a web address in the default browser, or a
// folder in Explorer), started by the desktop.
func openAsUser(address string) error {
	return desktopShell(func(shell *comObject) error {
		args := []variant{bstr(address), bstr(""), bstr(""), bstr("open"), {vt: vtI4, val: windows.SW_SHOWNORMAL}}
		for _, a := range args[:4] {
			if a.val == 0 {
				return errors.New("no memory for the address")
			}
		}
		defer func() {
			for i := range args {
				freeVariant(&args[i])
			}
		}()
		// the browser comes to the front, though Explorer starts it
		_, _, _ = procAllowSetForegroundWindow.Call(uintptr(^uint32(0)))
		r, err := shell.invoke("ShellExecute", dispatchMethod, args...)
		freeVariant(&r)
		return err
	})
}
