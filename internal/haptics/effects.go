package haptics

import "strings"

func both(v Voice) Voice  { v.L, v.R = 1, 1; return v }
func left(v Voice) Voice  { v.L, v.R = 1, 0; return v }
func right(v Voice) Voice { v.L, v.R = 0, 1; return v }

func click(f, amp, delay float64) Voice {
	return Voice{Wave: Square, F0: f, Amp: amp, Delay: delay, Attack: 0.001, Hold: 0.012, Release: 0.006}
}

// scanSweep: another ship scanning you. A buzzing scan line sweeps from
// the left grip to the right over a low rising hum, then a soft "done" tick.
func scanSweep() []Voice {
	vs := []Voice{
		both(Voice{Wave: Sine, F0: 70, F1: 140, Amp: 0.3, Attack: 0.15, Hold: 0.45, Release: 0.25}),
		both(Voice{Wave: Sine, F0: 210, Amp: 0.3, Delay: 0.95, Attack: 0.003, Hold: 0.03, Release: 0.06}),
	}
	const steps = 7
	for i := 0; i < steps; i++ {
		x := float64(i) / (steps - 1)
		vs = append(vs, Voice{Wave: NormNoise, F0: 420 + 260*x, Amp: 0.42, L: 1 - x, R: x,
			Delay: 0.1 * float64(i), Attack: 0.04, Hold: 0.05, Release: 0.09, TremHz: 28, TremDepth: 0.55})
	}
	return vs
}

// effects are the one-shots, by name. The DualSense actuators respond best
// around 30-250 Hz, strongest near 60 Hz.
var effects = map[string][]Voice{
	"scanned": scanSweep(),
	"hardpoints": {
		both(Voice{Wave: Square, F0: 110, Amp: 0.55, Attack: 0.002, Hold: 0.025, Release: 0.01}),
		both(Voice{Wave: Saw, F0: 60, F1: 75, Amp: 0.18, Delay: 0.03, Attack: 0.02, Hold: 0.18, Release: 0.05}),
		both(Voice{Wave: Square, F0: 85, Amp: 0.55, Delay: 0.26, Attack: 0.002, Hold: 0.03, Release: 0.02}),
	},
	"landing_gear": {
		both(Voice{Wave: Saw, F0: 55, F1: 45, Amp: 0.2, Attack: 0.05, Hold: 0.6, Release: 0.2, TremHz: 18, TremDepth: 0.3}),
		both(Voice{Wave: Square, F0: 40, Amp: 0.75, Delay: 0.8, Attack: 0.002, Hold: 0.06, Release: 0.08}),
	},
	"cargo_scoop": {
		both(Voice{Wave: Saw, F0: 65, F1: 50, Amp: 0.18, Attack: 0.05, Hold: 0.45, Release: 0.15}),
		both(Voice{Wave: Square, F0: 45, Amp: 0.55, Delay: 0.6, Attack: 0.002, Hold: 0.05, Release: 0.06}),
	},
	"silent_running": {both(Voice{Wave: Sine, F0: 120, F1: 40, Amp: 0.4, Attack: 0.01, Hold: 0.1, Release: 0.25})},
	"pips":           {right(click(180, 0.45, 0))},
	"fire_group":     {left(click(160, 0.45, 0))},
	"target_locked": {
		right(Voice{Wave: Sine, F0: 220, Amp: 0.4, Attack: 0.002, Hold: 0.02, Release: 0.01}),
		right(Voice{Wave: Sine, F0: 330, Amp: 0.3, Delay: 0.05, Attack: 0.002, Hold: 0.02, Release: 0.01}),
	},
	"fsd_jump": {
		both(Voice{Wave: Noise, F0: 400, Amp: 0.9, Attack: 0.005, Hold: 0.08, Release: 0.35}),
		both(Voice{Wave: Sine, F0: 60, F1: 25, Amp: 1, Attack: 0.005, Hold: 0.15, Release: 0.6}),
	},
	"supercruise_in": {
		both(Voice{Wave: Sine, F0: 70, F1: 35, Amp: 0.85, Attack: 0.01, Hold: 0.15, Release: 0.45}),
		both(Voice{Wave: Noise, F0: 250, Amp: 0.4, Attack: 0.01, Hold: 0.05, Release: 0.3}),
	},
	"supercruise_out": {
		both(Voice{Wave: Square, F0: 35, Amp: 0.9, Attack: 0.003, Hold: 0.12, Release: 0.5}),
		both(Voice{Wave: Noise, F0: 300, Amp: 0.7, Attack: 0.003, Hold: 0.1, Release: 0.5}),
		both(Voice{Wave: Square, F0: 30, Amp: 0.7, Delay: 0.25, Attack: 0.003, Hold: 0.08, Release: 0.3}),
	},
	"docked": {
		both(Voice{Wave: Square, F0: 38, Amp: 0.8, Attack: 0.002, Hold: 0.06, Release: 0.12}),
		both(Voice{Wave: Square, F0: 34, Amp: 0.8, Delay: 0.22, Attack: 0.002, Hold: 0.06, Release: 0.2}),
		both(Voice{Wave: Noise, F0: 500, Amp: 0.25, Delay: 0.3, Attack: 0.05, Hold: 0.2, Release: 0.4}),
	},
	"undocked": {
		both(Voice{Wave: Square, F0: 40, Amp: 0.7, Attack: 0.002, Hold: 0.05, Release: 0.15}),
		both(Voice{Wave: Noise, F0: 600, Amp: 0.3, Attack: 0.02, Hold: 0.3, Release: 0.5}),
	},
	"touchdown": {
		both(Voice{Wave: Sine, F0: 45, F1: 30, Amp: 0.9, Attack: 0.003, Hold: 0.08, Release: 0.4}),
		both(Voice{Wave: Noise, F0: 200, Amp: 0.5, Attack: 0.003, Hold: 0.05, Release: 0.25}),
	},
	"liftoff": {both(Voice{Wave: Sine, F0: 35, F1: 55, Amp: 0.45, Attack: 0.2, Hold: 0.2, Release: 0.4})},
	"vehicle": {
		both(Voice{Wave: Square, F0: 42, Amp: 0.7, Attack: 0.002, Hold: 0.06, Release: 0.2}),
		both(Voice{Wave: Saw, F0: 70, Amp: 0.2, Delay: 0.1, Attack: 0.05, Hold: 0.3, Release: 0.2}),
	},
	"shields_down": {
		both(Voice{Wave: Sine, F0: 200, F1: 30, Amp: 1, Attack: 0.005, Hold: 0.1, Release: 0.6}),
		both(Voice{Wave: Noise, F0: 800, Amp: 0.6, Attack: 0.005, Hold: 0.15, Release: 0.4, TremHz: 30, TremDepth: 0.8}),
	},
	"shields_up": {both(Voice{Wave: Sine, F0: 80, F1: 220, Amp: 0.45, Attack: 0.15, Hold: 0.1, Release: 0.25, TremHz: 12, TremDepth: 0.5})},
	"hull_hit": {
		both(Voice{Wave: Square, F0: 42, Amp: 1, Attack: 0.002, Hold: 0.09, Release: 0.25}),
		both(Voice{Wave: Noise, F0: 250, Amp: 0.6, Attack: 0.002, Hold: 0.06, Release: 0.2}),
	},
	// Elite logs UnderAttack only now and then (often a minute apart), so it
	// gets its own jolt: impact, shield crackle, echo.
	"under_attack": {
		both(Voice{Wave: Square, F0: 48, Amp: 0.9, Attack: 0.002, Hold: 0.05, Release: 0.15}),
		both(Voice{Wave: Noise, F0: 900, Amp: 0.5, Attack: 0.002, Hold: 0.08, Release: 0.25, TremHz: 35, TremDepth: 0.8}),
		both(Voice{Wave: Square, F0: 40, Amp: 0.55, Delay: 0.22, Attack: 0.002, Hold: 0.04, Release: 0.12}),
	},
	// heat sink launch: a hard pop, then a cold draining sweep
	"heat_sink": {
		both(Voice{Wave: Square, F0: 50, Amp: 0.8, Attack: 0.002, Hold: 0.04, Release: 0.1}),
		both(Voice{Wave: NormNoise, F0: 400, F1: 60, Amp: 0.55, Delay: 0.05, Attack: 0.02, Hold: 0.3, Release: 0.6}),
	},
	// chaff: a quick burst of crackles
	"chaff": {
		both(Voice{Wave: Square, F0: 45, Amp: 0.7, Attack: 0.002, Hold: 0.03, Release: 0.08}),
		both(Voice{Wave: NormNoise, F0: 900, Amp: 0.5, Delay: 0.04, Attack: 0.005, Hold: 0.45, Release: 0.25, GateHz: 22, GateDuty: 0.35}),
	},
	// shield cell: spin-up hum that climbs, then a shimmer as shields fill
	"shield_cell": {
		both(Voice{Wave: Sine, F0: 40, F1: 160, Amp: 0.55, Attack: 0.3, Hold: 1.6, Release: 0.3, TremHz: 9, TremDepth: 0.35}),
		both(Voice{Wave: Sine, F0: 180, F1: 240, Amp: 0.4, Delay: 1.9, Attack: 0.05, Hold: 0.3, Release: 0.5, TremHz: 14, TremDepth: 0.6}),
	},
	// flight assist: off = two falling ticks, on = two rising ticks
	"fa_off": {both(click(150, 0.5, 0)), both(click(95, 0.55, 0.09))},
	"fa_on":  {both(click(95, 0.5, 0)), both(click(150, 0.55, 0.09))},
	// FSD cooldown over: a short charged chirp on the right
	"fsd_ready": {
		right(Voice{Wave: Sine, F0: 90, F1: 220, Amp: 0.5, Attack: 0.005, Hold: 0.06, Release: 0.04}),
		right(click(260, 0.35, 0.12)),
	},
	// mass lock: a dull, heavy hold; release: a light let-go
	"mass_lock":   {both(Voice{Wave: Sine, F0: 34, Amp: 0.55, Attack: 0.03, Hold: 0.12, Release: 0.2})},
	"mass_unlock": {both(Voice{Wave: Sine, F0: 45, F1: 80, Amp: 0.35, Attack: 0.01, Hold: 0.05, Release: 0.1})},
	// atmosphere: hitting the air, and leaving the glide
	"glide_start": {
		both(Voice{Wave: Square, F0: 36, Amp: 0.8, Attack: 0.003, Hold: 0.08, Release: 0.3}),
		both(Voice{Wave: NormNoise, F0: 250, Amp: 0.5, Attack: 0.05, Hold: 0.4, Release: 0.6}),
	},
	"glide_end": {both(Voice{Wave: NormNoise, F0: 200, F1: 60, Amp: 0.45, Attack: 0.01, Hold: 0.15, Release: 0.5})},
	// cargo canister: scooped into the hold / pushed out of it
	"cargo_collect": {
		both(Voice{Wave: Square, F0: 60, Amp: 0.6, Attack: 0.002, Hold: 0.03, Release: 0.06}),
		both(Voice{Wave: Square, F0: 45, Amp: 0.7, Delay: 0.18, Attack: 0.002, Hold: 0.05, Release: 0.12}),
	},
	"cargo_eject": {
		both(Voice{Wave: Square, F0: 40, Amp: 0.75, Attack: 0.002, Hold: 0.05, Release: 0.1}),
		both(Voice{Wave: NormNoise, F0: 300, F1: 120, Amp: 0.45, Delay: 0.05, Attack: 0.01, Hold: 0.2, Release: 0.3}),
	},
	// player / wing message: phone-style double buzz
	"message": {
		both(Voice{Wave: Sine, F0: 170, Amp: 0.45, Attack: 0.005, Hold: 0.07, Release: 0.02}),
		both(Voice{Wave: Sine, F0: 170, Amp: 0.45, Delay: 0.18, Attack: 0.005, Hold: 0.07, Release: 0.02}),
	},
	// Thargoid shutdown field: power dies away; reboot: relays click back and power spins up
	"systems_shutdown": {
		both(Voice{Wave: Sine, F0: 120, F1: 12, Amp: 1, Attack: 0.005, Hold: 0.3, Release: 1.6}),
		both(Voice{Wave: NormNoise, F0: 700, Amp: 0.5, Attack: 0.005, Hold: 0.25, Release: 0.6, GateHz: 18, GateDuty: 0.4}),
	},
	"systems_reboot": {
		both(click(40, 0.6, 0)), both(click(60, 0.6, 0.35)), both(click(80, 0.6, 0.7)), both(click(110, 0.6, 1.05)),
		both(Voice{Wave: Sine, F0: 25, F1: 70, Amp: 0.6, Delay: 1.2, Attack: 0.6, Hold: 0.6, Release: 0.5}),
	},
	// sudden flick of the controller while flying: a soft, smooth push
	"maneuver_kick": {both(Voice{Wave: Sine, F0: 160, F1: 110, Amp: 0.4, Attack: 0.015, Hold: 0.03, Release: 0.15})},
	"kill": {
		both(Voice{Wave: Sine, F0: 150, Amp: 0.7, Attack: 0.002, Hold: 0.04, Release: 0.04}),
		both(Voice{Wave: Sine, F0: 190, Amp: 0.7, Delay: 0.12, Attack: 0.002, Hold: 0.05, Release: 0.08}),
	},
	"heat_warning": {
		both(click(100, 0.55, 0)), both(click(100, 0.55, 0.16)), both(click(100, 0.55, 0.32)),
	},
	"heat_damage": {both(Voice{Wave: Noise, F0: 300, Amp: 0.8, Attack: 0.01, Hold: 0.15, Release: 0.3, TremHz: 25, TremDepth: 0.7})},
	"died": {
		both(Voice{Wave: Sine, F0: 70, F1: 18, Amp: 1, Attack: 0.01, Hold: 0.5, Release: 2.0}),
		both(Voice{Wave: Noise, F0: 150, Amp: 0.6, Attack: 0.01, Hold: 0.3, Release: 1.5}),
	},
	"honk": {both(Voice{Wave: Sine, F0: 140, F1: 45, Amp: 0.7, Attack: 0.02, Hold: 0.3, Release: 1.2, TremHz: 9, TremDepth: 0.6})},
	"jet_cone": {
		both(Voice{Wave: Noise, F0: 180, Amp: 1, Attack: 0.05, Hold: 0.8, Release: 1.0, TremHz: 6, TremDepth: 0.5}),
		both(Voice{Wave: Sine, F0: 30, Amp: 0.8, Attack: 0.05, Hold: 0.8, Release: 1.0}),
	},
	"interdicted": {
		both(Voice{Wave: Sine, F0: 90, F1: 30, Amp: 0.9, Attack: 0.005, Hold: 0.2, Release: 0.5}),
		both(Voice{Wave: Noise, F0: 400, Amp: 0.5, Attack: 0.005, Hold: 0.2, Release: 0.4}),
	},
	"escaped": {both(Voice{Wave: Sine, F0: 40, F1: 120, Amp: 0.6, Attack: 0.05, Hold: 0.1, Release: 0.3})},
	"hull_breach": {
		both(Voice{Wave: Noise, F0: 900, Amp: 1, Attack: 0.005, Hold: 0.6, Release: 1.2, TremHz: 14, TremDepth: 0.6}),
		both(Voice{Wave: Square, F0: 35, Amp: 0.9, Attack: 0.003, Hold: 0.2, Release: 0.6}),
	},
	"limpet":          {both(Voice{Wave: Sine, F0: 180, F1: 90, Amp: 0.45, Attack: 0.002, Hold: 0.03, Release: 0.08})},
	"docking_granted": {both(Voice{Wave: Sine, F0: 180, Amp: 0.35, Attack: 0.002, Hold: 0.05, Release: 0.03}), both(Voice{Wave: Sine, F0: 240, Amp: 0.35, Delay: 0.12, Attack: 0.002, Hold: 0.05, Release: 0.05})},
	"boost": {
		both(Voice{Wave: Noise, F0: 260, Amp: 0.9, Attack: 0.03, Hold: 0.35, Release: 0.7}),
		both(Voice{Wave: Sine, F0: 45, F1: 85, Amp: 0.8, Attack: 0.05, Hold: 0.3, Release: 0.7}),
	},
	// weapon one-shots (side chosen at play time)
	"rail_crack": {
		{Wave: Noise, F0: 1500, Amp: 1, Attack: 0.001, Hold: 0.03, Release: 0.12},
		{Wave: Square, F0: 45, Amp: 1, Attack: 0.001, Hold: 0.06, Release: 0.3},
	},
	"missile_launch": {
		{Wave: Noise, F0: 500, Amp: 0.8, Attack: 0.01, Hold: 0.2, Release: 0.4},
		{Wave: Sine, F0: 60, F1: 40, Amp: 0.6, Attack: 0.01, Hold: 0.15, Release: 0.3},
	},
	"shot_kinetic": {
		{Wave: Square, F0: 80, Amp: 0.9, Attack: 0.001, Hold: 0.02, Release: 0.06},
		{Wave: Noise, F0: 1200, Amp: 0.4, Attack: 0.001, Hold: 0.01, Release: 0.04},
	},
	"shot_laser": {{Wave: Sine, F0: 180, F1: 140, Amp: 0.65, Attack: 0.002, Hold: 0.03, Release: 0.05}},
	"shot_plasma": {
		{Wave: Sine, F0: 50, F1: 30, Amp: 1, Attack: 0.002, Hold: 0.08, Release: 0.25},
		{Wave: Noise, F0: 300, Amp: 0.5, Attack: 0.002, Hold: 0.05, Release: 0.2},
	},
}

// Effect returns a one-shot effect's voices.
func Effect(name string) ([]Voice, bool) {
	vs, ok := effects[name]
	return vs, ok
}

// WeaponTexture is the feel of a weapon class while its trigger is held.
func WeaponTexture(class string) Voice {
	switch class {
	case "beam":
		return Voice{Wave: Sine, F0: 110, Amp: 0.45, TremHz: 7, TremDepth: 0.25}
	case "pulse":
		return Voice{Wave: Sine, F0: 160, Amp: 0.7, GateHz: 6, GateDuty: 0.3}
	case "burst":
		return Voice{Wave: Sine, F0: 170, Amp: 0.7, GateHz: 14, GateDuty: 0.35}
	case "multicannon":
		return Voice{Wave: Square, F0: 55, Amp: 0.75, GateHz: 11, GateDuty: 0.35}
	case "cannon":
		return Voice{Wave: Square, F0: 38, Amp: 1, GateHz: 1.2, GateDuty: 0.12}
	case "fragment":
		return Voice{Wave: Noise, F0: 350, Amp: 0.9, GateHz: 2.5, GateDuty: 0.15}
	case "plasma":
		return Voice{Wave: Sine, F0: 35, Amp: 1, GateHz: 0.8, GateDuty: 0.2}
	case "mining":
		return Voice{Wave: Saw, F0: 90, Amp: 0.4, TremHz: 13, TremDepth: 0.4}
	case "missile", "railgun":
		return Voice{} // handled as one-shots
	default:
		return Voice{Wave: Sine, F0: 90, Amp: 0.5, GateHz: 8, GateDuty: 0.4}
	}
}

// onFootShot is the shot effect of an on-foot weapon (Status.json
// SelectedWeapon); empty for tools and fists.
func onFootShot(weapon string) string {
	w := strings.ToLower(weapon)
	switch {
	case w == "" || strings.Contains(w, "fists") || strings.Contains(w, "tool") || strings.Contains(w, "scanner") || strings.Contains(w, "cutter") || strings.Contains(w, "energylink"):
		return ""
	case strings.Contains(w, "plasma") || strings.Contains(w, "manticore") || strings.Contains(w, "executioner") || strings.Contains(w, "intimidator") || strings.Contains(w, "oppressor") || strings.Contains(w, "tormentor"):
		return "shot_plasma"
	case strings.Contains(w, "laser") || strings.Contains(w, "takada") || strings.Contains(w, "aphelion") || strings.Contains(w, "eclipse") || strings.Contains(w, "zenith") || strings.Contains(w, "tk_"):
		return "shot_laser"
	default:
		return "shot_kinetic"
	}
}
