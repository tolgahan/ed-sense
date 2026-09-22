package elite

import "strings"

// Module is a weapon on a hardpoint.
type Module struct {
	Class string // weapon class (beam, multicannon ...)
	Size  int    // 4 huge .. 1 small
}

// LoadoutModules returns the weapons of a Loadout event: hardpoints,
// excluding the utility mounts.
func LoadoutModules(ev Event) []Module {
	var out []Module
	mods, _ := ev["Modules"].([]any)
	for _, raw := range mods {
		m, _ := raw.(map[string]any)
		entry := Event(m)
		slot := strings.ToLower(entry.Text("Slot"))
		if !isWeaponSlot(slot) {
			continue
		}
		out = append(out, Module{Class: WeaponClass(entry.Text("Item")), Size: slotSize(slot)})
	}
	return out
}

// isWeaponSlot: hardpoints, excluding the utility mounts.
func isWeaponSlot(slot string) bool {
	return strings.Contains(slot, "hardpoint") && !strings.HasPrefix(slot, "tinyhardpoint")
}

func slotSize(slot string) int {
	switch {
	case strings.HasPrefix(slot, "huge"):
		return 4
	case strings.HasPrefix(slot, "large"):
		return 3
	case strings.HasPrefix(slot, "medium"):
		return 2
	default:
		return 1
	}
}

// WeaponClass maps a Loadout item to the weapon class its feel is based on.
func WeaponClass(item string) string {
	i := strings.ToLower(item)
	switch {
	case strings.Contains(i, "beamlaser"):
		return "beam"
	case strings.Contains(i, "pulselaserburst"):
		return "burst"
	case strings.Contains(i, "pulselaser"):
		return "pulse"
	case strings.Contains(i, "mining") || strings.Contains(i, "subsurface"):
		return "mining"
	case strings.Contains(i, "multicannon"):
		return "multicannon"
	case strings.Contains(i, "slugshot"):
		return "fragment"
	case strings.Contains(i, "railgun"):
		return "railgun"
	case strings.Contains(i, "plasmaaccelerator") || strings.Contains(i, "guardian_plasma"):
		return "plasma"
	case strings.Contains(i, "missile") || strings.Contains(i, "torpedo") || strings.Contains(i, "minelauncher") || strings.Contains(i, "dumbfire"):
		return "missile"
	case strings.Contains(i, "cannon") || strings.Contains(i, "flakmortar"):
		return "cannon"
	case strings.Contains(i, "guardian"):
		return "plasma"
	default:
		return "generic"
	}
}
