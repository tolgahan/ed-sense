package dualsense

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native haptics go to the virtual DualSense's 4-channel audio device
// through the classic waveOut API: channels 3 and 4 drive the left and right
// actuators, and DSX passes them through to the real controller.

var (
	winmm                      = windows.NewLazySystemDLL("winmm.dll")
	procWaveOutGetNumDevs      = winmm.NewProc("waveOutGetNumDevs")
	procWaveOutGetDevCaps      = winmm.NewProc("waveOutGetDevCapsW")
	procWaveOutMessage         = winmm.NewProc("waveOutMessage")
	procWaveOutOpen            = winmm.NewProc("waveOutOpen")
	procWaveOutPrepareHeader   = winmm.NewProc("waveOutPrepareHeader")
	procWaveOutWrite           = winmm.NewProc("waveOutWrite")
	procWaveOutUnprepareHeader = winmm.NewProc("waveOutUnprepareHeader")
	procWaveOutReset           = winmm.NewProc("waveOutReset")
	procWaveOutClose           = winmm.NewProc("waveOutClose")

	procSetThreadPriority = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadPriority")
)

const (
	drvQueryFunctionInstanceID     = 0x0800 + 17
	drvQueryFunctionInstanceIDSize = 0x0800 + 18
	callbackEvent                  = 0x00050000
	whdrDone                       = 0x00000001
	threadPriorityTimeCritical     = 15
	bufferFrames                   = SampleRate / 100 // 10 ms
	bufferCount                    = 6
)

type waveOutCaps struct {
	Mid, Pid      uint16
	DriverVersion uint32
	Pname         [32]uint16
	Formats       uint32
	Channels      uint16
	Reserved1     uint16
	Support       uint32
}

type waveHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

// ListAudio enumerates the audio outputs.
func ListAudio() []AudioDevice {
	n, _, _ := procWaveOutGetNumDevs.Call()
	var out []AudioDevice
	for id := range int(n) {
		var caps waveOutCaps
		if r, _, _ := procWaveOutGetDevCaps.Call(uintptr(id), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); r != 0 {
			continue
		}
		d := AudioDevice{ID: id, Name: windows.UTF16ToString(caps.Pname[:]), Channels: int(caps.Channels)}
		inst := audioInstanceID(id)
		if strings.HasPrefix(inst, "{0.0.") {
			inst = `SWD\MMDEVAPI\` + inst
		}
		parents := deviceParents(inst, 10)
		d.Sony = strings.Contains(strings.ToUpper(inst+"|"+strings.Join(parents, "|")), "VID_054C") ||
			strings.Contains(strings.ToLower(d.Name), "dualsense")
		d.Kind = classify("", parents)
		out = append(out, d)
	}
	return out
}

func audioInstanceID(id int) string {
	var size uint32
	procWaveOutMessage.Call(uintptr(id), drvQueryFunctionInstanceIDSize, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 || size >= 4096 {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if r, _, _ := procWaveOutMessage.Call(uintptr(id), drvQueryFunctionInstanceID, uintptr(unsafe.Pointer(&buf[0])), uintptr(size)); r != 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func pickVirtualAudio(devices []AudioDevice) (AudioDevice, bool) {
	for _, kind := range []string{Virtual, Unknown} {
		for _, d := range devices {
			if d.Sony && d.Kind == kind {
				return d, true
			}
		}
	}
	return AudioDevice{}, false
}

// waveFormat4ch builds a WAVEFORMATEXTENSIBLE: 4 channels, 16 bit, SampleRate.
func waveFormat4ch(channelMask uint32) []byte {
	b := make([]byte, 40)
	le16 := func(o int, v uint16) { b[o], b[o+1] = byte(v), byte(v>>8) }
	le32 := func(o int, v uint32) { b[o], b[o+1], b[o+2], b[o+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24) }
	le16(0, 0xFFFE) // WAVE_FORMAT_EXTENSIBLE
	le16(2, 4)
	le32(4, SampleRate)
	le32(8, SampleRate*8)
	le16(12, 8)
	le16(14, 16)
	le16(16, 22)
	le16(18, 16)
	le32(20, channelMask)
	copy(b[24:], []byte{0x01, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xAA, 0, 0x38, 0x9B, 0x71}) // KSDATAFORMAT_SUBTYPE_PCM
	return b
}

// HapticsOut streams PCM from render to the virtual DualSense.
type HapticsOut struct {
	render  func(frames []int16)
	mu      sync.Mutex
	active  atomic.Bool
	running bool
	stop    chan struct{}
	last    time.Time
	warned  bool
}

func NewHapticsOut(render func(frames []int16)) *HapticsOut {
	return &HapticsOut{render: render}
}

// Active reports whether audio is streaming.
func (a *HapticsOut) Active() bool { return a.active.Load() }

// Maintain starts streaming when the device is there, checking every 3 s.
func (a *HapticsOut) Maintain() {
	a.mu.Lock()
	if a.running || time.Since(a.last) < 3*time.Second {
		a.mu.Unlock()
		return
	}
	a.last = time.Now()
	a.mu.Unlock()
	d, ok := pickVirtualAudio(ListAudio())
	a.mu.Lock()
	defer a.mu.Unlock()
	if !ok {
		if !a.warned {
			log.Print("Native haptics: no virtual DualSense audio device (needs DSX's DualSense emulation); using rumble")
			a.warned = true
		}
		return
	}
	a.running, a.stop = true, make(chan struct{})
	go a.run(d, a.stop)
}

func (a *HapticsOut) Close() {
	a.mu.Lock()
	if a.stop != nil {
		close(a.stop)
		a.stop = nil
	}
	a.mu.Unlock()
	for i := 0; i < 50 && a.active.Load(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *HapticsOut) run(d AudioDevice, stop chan struct{}) {
	if err := a.stream(d, stop); err != nil {
		log.Printf("Native haptics: %s: %v", d.Name, err)
	}
	a.active.Store(false)
	a.mu.Lock()
	a.running, a.last = false, time.Now()
	a.mu.Unlock()
}

func (a *HapticsOut) stream(d AudioDevice, stop chan struct{}) error {
	// an audio thread at time-critical priority, so buffers are refilled on time
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procSetThreadPriority.Call(uintptr(windows.CurrentThread()), threadPriorityTimeCritical)
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)

	h, mask, err := openWaveOut(d.ID, event)
	if err != nil {
		return err
	}
	log.Printf("Native haptics: streaming to %s (channel mask 0x%X)", d.Name, mask)
	defer func() {
		procWaveOutReset.Call(h)
		procWaveOutClose.Call(h)
	}()

	headers := make([]*waveHdr, bufferCount)
	buffers := make([][]int16, bufferCount)
	for i := range headers {
		buffers[i] = make([]int16, bufferFrames*4)
		headers[i] = &waveHdr{Data: uintptr(unsafe.Pointer(&buffers[i][0])), BufferLength: bufferFrames * 8}
		if r, _, _ := procWaveOutPrepareHeader.Call(h, uintptr(unsafe.Pointer(headers[i])), unsafe.Sizeof(waveHdr{})); r != 0 {
			return fmt.Errorf("waveOutPrepareHeader failed (%d)", r)
		}
	}
	defer func() {
		procWaveOutReset.Call(h)
		for _, hdr := range headers {
			procWaveOutUnprepareHeader.Call(h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{}))
		}
	}()
	write := func(i int) error {
		a.render(buffers[i])
		atomic.StoreUint32(&headers[i].Flags, atomic.LoadUint32(&headers[i].Flags)&^whdrDone)
		if r, _, _ := procWaveOutWrite.Call(h, uintptr(unsafe.Pointer(headers[i])), unsafe.Sizeof(waveHdr{})); r != 0 {
			return fmt.Errorf("waveOutWrite failed (%d)", r)
		}
		return nil
	}
	for i := range headers {
		if err := write(i); err != nil {
			return err
		}
	}
	a.active.Store(true)
	// every buffer played before the refill: the actuators went still for a
	// moment, which makes continuous effects feel rough
	dry, since := 0, time.Now()
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		windows.WaitForSingleObject(event, 50)
		played := 0
		for i := range headers {
			if atomic.LoadUint32(&headers[i].Flags)&whdrDone != 0 {
				if err := write(i); err != nil {
					return err
				}
				played++
			}
		}
		if played == bufferCount {
			dry++
		}
		if time.Since(since) >= time.Minute {
			if dry > 0 {
				log.Printf("Native haptics: the stream ran dry %d times in the last minute", dry)
			}
			dry, since = 0, time.Now()
		}
	}
}

// openWaveOut tries the 4-channel layouts drivers accept: quad, 4.0 side,
// surround, unspecified.
func openWaveOut(deviceID int, event windows.Handle) (uintptr, uint32, error) {
	var h uintptr
	for _, mask := range []uint32{0x33, 0x603, 0x107, 0} {
		format := waveFormat4ch(mask)
		r, _, _ := procWaveOutOpen.Call(uintptr(unsafe.Pointer(&h)), uintptr(deviceID), uintptr(unsafe.Pointer(&format[0])), uintptr(event), 0, callbackEvent)
		if r == 0 {
			return h, mask, nil
		}
	}
	return 0, 0, fmt.Errorf("waveOutOpen failed for every 4-channel layout")
}
