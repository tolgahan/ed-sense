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
	Parents       []string
}

func (d HIDDevice) String() string {
	return fmt.Sprintf("054C:%04X %s %q in %d out %d %s", d.ProductID, d.Kind, d.Product, d.InLen, d.OutLen, d.Path)
}

// AudioDevice is one audio output.
type AudioDevice struct {
	ID       int
	Name     string
	Channels int
	Kind     string // Physical, Virtual or Unknown
	Sony     bool
}

func (d AudioDevice) String() string {
	return fmt.Sprintf("#%d %q, %d channels, %s, Sony %v", d.ID, d.Name, d.Channels, d.Kind, d.Sony)
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
// Bluetooth stack; DSX's virtual pad does not.
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
