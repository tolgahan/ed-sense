package dualsense

import (
	"fmt"
	"strings"
)

// HIDDevice is one Sony HID interface.
type HIDDevice struct {
	Path          string
	ProductID     uint16
	Product       string
	InLen, OutLen int    // report lengths
	Kind          string // Physical, Virtual or Unknown
	Host          string // HostUSBIPWin2 for DS4Windows' virtual pads, else ""
	USBParent     string // the first USB\VID_054C&PID_xxxx\<serial> above it
	Parents       []string
}

func (d HIDDevice) String() string {
	return fmt.Sprintf("054C:%04X %s%s %q in %d out %d %s", d.ProductID, d.Kind, via(d.Host), d.Product, d.InLen, d.OutLen, d.Path)
}

// IsDualSense: a DualSense or a DualSense Edge.
func (d HIDDevice) IsDualSense() bool { return d.ProductID == 0x0CE6 || d.ProductID == 0x0DF2 }

// AudioDevice is one audio output.
type AudioDevice struct {
	ID       int
	Name     string
	Channels int
	Kind     string // Physical, Virtual or Unknown
	Host     string // as HIDDevice.Host
	Sony     bool
}

func (d AudioDevice) String() string {
	return fmt.Sprintf("#%d %q, %d channels, %s%s, Sony %v", d.ID, d.Name, d.Channels, d.Kind, via(d.Host), d.Sony)
}

func via(host string) string {
	if host == "" {
		return ""
	}
	return " (" + host + ")"
}

// HostUSBIPWin2: the device hangs under usbip-win2's virtual host
// controller, where DS4Windows 5 (VIIPER) puts its virtual pads.
const HostUSBIPWin2 = "usbip-win2"

// ancestor is a device above another in the device tree.
type ancestor struct {
	ID       string   // its instance ID, such as ROOT\USB\0000
	Hardware []string // its hardware IDs
	Service  string   // its driver's service
}

func ancestorIDs(up []ancestor) []string {
	out := make([]string, 0, len(up))
	for _, a := range up {
		out = append(out, a.ID)
	}
	return out
}

// hostOf names the virtual host controller a device hangs under. The
// usbip-win2 controller's instance ID is only ROOT\USB\000N, so it is told
// by its hardware ID or its driver.
func hostOf(up []ancestor) string {
	for _, a := range up {
		if strings.EqualFold(strings.TrimSpace(a.Service), "usbip2_ude") {
			return HostUSBIPWin2
		}
		for _, h := range a.Hardware {
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(h)), `ROOT\USBIP_WIN2\UDE`) {
				return HostUSBIPWin2
			}
		}
	}
	return ""
}

// kindOf is a device's kind and virtual host, by the devices above it: one
// under usbip-win2 is virtual, else classify tells.
func kindOf(path string, up []ancestor) (kind, host string) {
	if host = hostOf(up); host != "" {
		return Virtual, host
	}
	return classify(path, ancestorIDs(up)), ""
}

// usbParent: the first ancestor that is a Sony USB device itself (not one
// of its interfaces), which a pad's HID and audio functions share.
func usbParent(parents []string) string {
	for _, p := range parents {
		u := strings.ToUpper(p)
		if strings.HasPrefix(u, `USB\VID_054C&PID_`) && !strings.Contains(u, "&MI_") {
			return p
		}
	}
	return ""
}

// Device kinds, told apart by the device's parents in the device tree.
const (
	Physical = "physical"
	Virtual  = "virtual"
	Unknown  = "unknown"
)

// instanceID turns a device interface path into its device instance ID:
// \\?\HID#VID_054C&PID_0CE6&MI_03#4&x&0&0000#{guid} -> HID\VID_054C&PID_0CE6&MI_03\4&x&0&0000
func instanceID(path string) string {
	p := strings.TrimPrefix(path, `\\?\`)
	if i := strings.LastIndex(p, "#{"); i >= 0 {
		p = p[:i]
	}
	return strings.ReplaceAll(p, "#", `\`)
}

// classify: real controllers sit under a USB host controller or the
// Bluetooth stack; DSX's and DS4Windows' virtual pads do not.
func classify(path string, parents []string) string {
	up := strings.ToUpper(strings.Join(parents, "|"))
	switch {
	case strings.Contains(strings.ToLower(path), "00001124-0000-1000-8000-00805f9b34fb") || strings.Contains(up, "BTH"):
		return Physical
	case strings.Contains(up, "NEFARIUS") || strings.Contains(up, "VPAD") || strings.Contains(up, "VIRTUAL"):
		return Virtual
	case strings.Contains(up, `PCI\`) || strings.Contains(up, `ACPI\`):
		return Physical
	case len(parents) > 0:
		return Virtual
	default:
		return Unknown
	}
}

// pickVirtual returns the virtual DualSense: one under the prefer host
// first (DS4Windows' under usbip-win2), then any virtual one, then one of
// unknown kind.
func pickVirtual(devices []HIDDevice, prefer string) (HIDDevice, bool) {
	usable := func(d HIDDevice) bool { return d.IsDualSense() && d.InLen > 0 && d.OutLen > 0 }
	if prefer != "" {
		for _, d := range devices {
			if usable(d) && d.Host == prefer {
				return d, true
			}
		}
	}
	for _, kind := range []string{Virtual, Unknown} {
		for _, d := range devices {
			if usable(d) && d.Kind == kind {
				return d, true
			}
		}
	}
	return HIDDevice{}, false
}

// AudioRoute is where native haptics go.
type AudioRoute int

const (
	RouteVirtual    AudioRoute = iota // the virtual DualSense's audio device
	RouteController                   // the controller's own (wired only)
	RouteAuto                         // the controller's own if there is one, else the virtual one
)

// pickAudio returns the audio device for route; a virtual one under the
// prefer host first.
func pickAudio(devices []AudioDevice, route AudioRoute, prefer string) (AudioDevice, bool) {
	if route != RouteVirtual {
		for _, d := range devices {
			if d.Sony && d.Kind == Physical {
				return d, true
			}
		}
		if route == RouteController {
			return AudioDevice{}, false
		}
	}
	if prefer != "" {
		for _, d := range devices {
			if d.Sony && d.Host == prefer {
				return d, true
			}
		}
	}
	for _, kind := range []string{Virtual, Unknown} {
		for _, d := range devices {
			if d.Sony && d.Kind == kind {
				return d, true
			}
		}
	}
	return AudioDevice{}, false
}

// LinkOptions choose the virtual DualSense a Link opens.
type LinkOptions struct {
	Prefer  string // a host whose pads come first (HostUSBIPWin2 for DS4Windows)
	Missing string // logged once when there is none
	// StopClears: a stop is sent with the rumble bits off (ReleaseReport),
	// and once when the pad opens, so the real controller goes back to
	// native haptics. DS4Windows keeps the rumble bits of the last report.
	StopClears bool
}

// AudioOptions choose the audio device native haptics go to.
type AudioOptions struct {
	Route   func() AudioRoute // asked at each try, so a change applies; nil: RouteVirtual
	Prefer  string            // as LinkOptions.Prefer
	Missing string            // logged once when there is none
}
