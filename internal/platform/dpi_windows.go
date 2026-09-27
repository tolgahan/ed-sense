package platform

// MakeDPIAware stops Windows from scaling this process' coordinates on
// high-DPI screens, which would blur and offset screen captures.
func MakeDPIAware() {
	const perMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	if p := user32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		if ok, _, _ := p.Call(perMonitorV2); ok != 0 {
			return
		}
	}
	if p := user32.NewProc("SetProcessDPIAware"); p.Find() == nil {
		p.Call()
	}
}
