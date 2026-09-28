package elite

import (
	"fmt"
	"slices"
	"strings"
)

// Module is a ship module that can be put in a fire group.
type Module struct {
	Name     string // as the HUD's fire group lists show it
	Class    string // weapon class (beam, multicannon ...) or utility class (heatsink, chaff ...)
	Utility  bool
	Ammo     bool
	AmmoText string // the lists' ammo line as the Loadout has it, e.g. "77/2100"
	Size     int    // weapons: 4 huge .. 1 small
}

// LoadoutModules returns the fire-groupable modules of a Loadout event.
func LoadoutModules(ev Event) []Module {
	var out []Module
	mods, _ := ev["Modules"].([]any)
	for _, raw := range mods {
		m, _ := raw.(map[string]any)
		entry := Event(m)
		mod, ok := fireGroupModule(entry.Text("Slot"), entry.Text("Item"))
		if !ok {
			continue
		}
		if clip, ok := entry.Number("AmmoInClip"); ok {
			hopper, _ := entry.Number("AmmoInHopper")
			mod.Ammo = true
			mod.AmmoText = fmt.Sprintf("%d/%d", int(clip), int(hopper))
		}
		out = append(out, mod)
	}
	return out
}

func fireGroupModule(slot, item string) (Module, bool) {
	slot, item = strings.ToLower(slot), strings.ToLower(item)
	for _, u := range utilityModules {
		if strings.Contains(item, u.item) {
			return Module{Name: u.name, Class: u.class, Utility: true}, true
		}
	}
	if !isWeaponSlot(slot) {
		return Module{}, false
	}
	name := strings.ToUpper(strings.TrimPrefix(item, "hpt_"))
	for _, w := range weaponNames {
		if strings.Contains(item, w.item) {
			name = w.name
			break
		}
	}
	return Module{Name: name, Class: WeaponClass(item), Size: slotSize(slot)}, true
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

var utilityModules = []struct{ item, name, class string }{
	{"heatsinklauncher", "HEATSINK", "heatsink"},
	{"causticsinklauncher", "CAUSTIC SINK", "heatsink"},
	{"chafflauncher", "CHAFF", "chaff"},
	{"electroniccountermeasure", "ECM", "ecm"},
	{"plasmapointdefence", "POINT DEFENCE", "pointdefence"},
	{"crimescanner", "KILL WARRANT SCANNER", "scanner"},
	{"cargoscanner", "MANIFEST SCANNER", "scanner"},
	{"cloudscanner", "FRAME SHIFT WAKE SCANNER", "scanner"},
	{"xenoscanner", "XENO SCANNER", "scanner"},
	{"mrascanner", "PULSE WAVE ANALYSER", "scanner"},
	{"antiunknownshutdown", "SHUTDOWN FIELD NEUTRALISER", "ecm"},
	{"shieldcellbank", "SHIELD CELL BANK", "shieldcell"},
	{"dronecontrol_collection", "COLLECTOR LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_prospector", "PROSPECTOR LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_fueltransfer", "FUEL TRANSFER LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_resourcesiphon", "HATCH BREAKER LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_repair", "REPAIR LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_recon", "RECON LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_research", "RESEARCH LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_unkvesselresearch", "RESEARCH LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_decontamination", "DECONTAMINATION LIMPET CONTROLLER", "limpet"},
	{"dronecontrol_rescue", "RESCUE LIMPET CONTROLLER", "limpet"},
	{"multidronecontrol", "MULTI LIMPET CONTROLLER", "limpet"},
}

// weaponNames: the first match wins, so special versions come first.
var weaponNames = []struct{ item, name string }{
	{"beamlaser_fixed_small_heat", "RETRIBUTOR"},
	{"pulselaserburst_fixed_small_scatter", "CYTOSCRAMBLER"},
	{"pulselaser_fixed_medium_disruptor", "PULSE DISRUPTOR"},
	{"multicannon_fixed_small_strong", "ENFORCER"},
	{"multicannon_fixed_medium_advanced", "ADVANCED MULTI-CANNON"},
	{"railgun_fixed_medium_burst", "IMPERIAL HAMMER"},
	{"slugshot_fixed_large_range", "PACIFIER"},
	{"plasmaaccelerator_fixed_large_advanced", "ADVANCED PLASMA ACCELERATOR"},
	{"mininglaser_fixed_small_advanced", "MINING LANCE"},
	{"dumbfiremissilerack_fixed_medium_lasso", "ROCKET PROPELLED FSD DISRUPTOR"},
	{"dumbfiremissilerack_fixed_medium_advanced", "ADVANCED MISSILE RACK"},
	{"causticmissile", "ENZYME MISSILE RACK"},
	{"drunkmissilerack", "PACK-HOUND MISSILE RACK"},
	{"atdumbfiremissile", "AX MISSILE RACK"},
	{"atmulticannon", "AX MULTI-CANNON"},
	{"atventdisruptorpylon", "TORPEDO PYLON"},
	{"advancedtorppylon", "TORPEDO PYLON"},
	{"basicmissilerack", "SEEKER MISSILE RACK"},
	{"dumbfiremissilerack", "MISSILE RACK"},
	{"minelauncher_fixed_small_impulse", "SHOCK MINE LAUNCHER"},
	{"minelauncher", "MINE LAUNCHER"},
	{"flakmortar", "REMOTE RELEASE FLAK LAUNCHER"},
	{"flechettelauncher", "REMOTE RELEASE FLECHETTE LAUNCHER"},
	{"plasmashock", "SHOCK CANNON"},
	{"guardian_plasmalauncher", "GUARDIAN PLASMA CHARGER"},
	{"guardian_gausscannon", "GUARDIAN GAUSS CANNON"},
	{"guardian_shardcannon", "GUARDIAN SHARD CANNON"},
	{"mining_abrblstr", "ABRASION BLASTER"},
	{"mining_seismchrgwarhd", "SEISMIC CHARGE LAUNCHER"},
	{"mining_subsurfdispmisle", "SUB-SURFACE DISPLACEMENT MISSILE"},
	{"mininglaser", "MINING LASER"},
	{"beamlaser", "BEAM LASER"},
	{"pulselaserburst", "BURST LASER"},
	{"pulselaser", "PULSE LASER"},
	{"multicannon", "MULTI-CANNON"},
	{"slugshot", "FRAGMENT CANNON"},
	{"railgun", "RAIL GUN"},
	{"plasmaaccelerator", "PLASMA ACCELERATOR"},
	{"cannon", "CANNON"},
}

// SameModules: the same fire-groupable modules in the same slots.
func SameModules(a, b []Module) bool {
	return slices.EqualFunc(a, b, func(x, y Module) bool { return x.Name == y.Name && x.Utility == y.Utility })
}
