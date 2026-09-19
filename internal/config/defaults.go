package config

// Default returns the settings a new file starts with.
func Default() Config {
	return Config{
		Version:         Version,
		PollMs:          25,
		Lightbar:        true,
		Triggers:        true,
		PlayerLEDs:      true,
		MicLED:          true,
		Haptics:         true,
		HapticsStrength: 1,
		HapticsGain:     defaultGains(),
		Brightness:      200,
		Colors:          defaultColors(),
		TriggerFX:       defaultTriggers(),
		Rumble:          defaultRumble(),
	}
}

func defaultColors() map[string][3]int {
	return map[string][3]int{
		"hull_full":    {0, 255, 60},
		"hull_half":    {255, 190, 0},
		"hull_low":     {255, 0, 0},
		"supercruise":  {30, 80, 255},
		"hyperspace":   {140, 170, 255},
		"docked":       {200, 200, 255},
		"srv":          {0, 200, 160},
		"fsd_charge":   {255, 255, 255},
		"overheat":     {255, 90, 0},
		"interdiction": {255, 0, 200},
		"fuel_scoop":   {255, 150, 0},
		"shields_down": {255, 0, 0},
		"shields_up":   {0, 160, 255},
		"hit":          {255, 30, 30},
		"kill":         {0, 255, 0},
		"jet_cone":     {0, 255, 255},
		"died":         {255, 0, 0},
		"docking_ok":   {0, 255, 80},
		"docking_no":   {255, 40, 0},
		"low_oxygen":   {0, 120, 255},
	}
}

// defaultTriggers: DSX v3 modes and their parameters (see README).
func defaultTriggers() map[string]Trigger {
	return map[string]Trigger{
		"ship_weapons_r":  {"WEAPON", []int{2, 5, 6}}, // hardpoints out: R2 primary fire
		"ship_weapons_l":  {"WEAPON", []int{2, 5, 4}}, // L2 secondary fire
		"ship_scanner_r":  {"FEEDBACK", []int{2, 3}},  // analysis mode, hardpoints out
		"ship_scanner_l":  {"FEEDBACK", []int{2, 3}},
		"ship_overheat_r": {"VIBRATION", []int{2, 6, 12}},
		"ship_overheat_l": {"VIBRATION", []int{2, 6, 12}},
		"interdiction":    {"VIBRATION", []int{1, 8, 30}},
		"hit":             {"VIBRATION", []int{1, 5, 25}}, // short buzz when hit
		"jet_cone":        {"VIBRATION", []int{1, 8, 8}},
		"srv_turret_r":    {"WEAPON", []int{2, 5, 5}},
		"srv_turret_l":    {"FEEDBACK", []int{2, 2}},
		"onfoot_r":        {"WEAPON", []int{2, 6, 5}}, // outside social spaces
		"onfoot_l":        {"FEEDBACK", []int{3, 2}},
	}
}

// defaultGains lists every haptic effect, so each can be tuned in the file.
func defaultGains() map[string]float64 {
	effects := []string{
		// weapons and flying
		"fire_primary", "fire_secondary",
		"thrust", "boost",
		// ship systems
		"hardpoints", "landing_gear", "cargo_scoop", "silent_running", "pips", "fire_group", "target_locked",
		"limpet",
		"heat_warning", "heat_damage", "overheat",
		// travel
		"fsd_charge", "fsd_jump", "hyperspace", "supercruise_in", "supercruise_out",
		"interdicted", "interdiction", "escaped", "jet_cone",
		"fuel_scoop", "honk",
		// planets and stations
		"docked", "undocked", "touchdown", "liftoff", "vehicle", "docking_granted",
		// combat
		"shields_down", "shields_up",
		"hull_hit", "hull_breach", "under_attack", "kill", "died", "scanned",
		// the rest
		"onfoot_shot",
	}
	g := make(map[string]float64, len(effects))
	for _, e := range effects {
		g[e] = 1
	}
	return g
}

// defaultRumble: the rumble of each effect: left and right strength (0-1)
// and, for one-shots, length. Continuous effects use their level as a
// fraction of these peaks.
func defaultRumble() map[string]Rumble {
	return map[string]Rumble{
		"fire_primary":    {0, 0.22, 0},
		"fire_secondary":  {0.22, 0, 0},
		"thrust":          {0.08, 0.08, 0},
		"fsd_charge":      {0.35, 0.35, 0},
		"hyperspace":      {0.10, 0.10, 0},
		"fuel_scoop":      {0.10, 0.04, 0},
		"overheat":        {0.30, 0.30, 0},
		"interdiction":    {0.55, 0.55, 0},
		"boost":           {0.70, 0.70, 900},
		"hardpoints":      {0.35, 0.35, 150},
		"landing_gear":    {0.40, 0.40, 300},
		"cargo_scoop":     {0.25, 0.25, 180},
		"silent_running":  {0.20, 0.20, 150},
		"pips":            {0, 0.18, 50},
		"fire_group":      {0.18, 0, 50},
		"target_locked":   {0, 0.12, 40},
		"fsd_jump":        {0.65, 0.65, 450},
		"supercruise_in":  {0.45, 0.45, 350},
		"supercruise_out": {0.75, 0.75, 500},
		"docked":          {0.50, 0.50, 350},
		"undocked":        {0.35, 0.35, 300},
		"touchdown":       {0.60, 0.60, 400},
		"liftoff":         {0.30, 0.30, 300},
		"vehicle":         {0.45, 0.45, 350},
		"shields_down":    {1.00, 1.00, 600},
		"shields_up":      {0.25, 0.25, 80},
		"hull_hit":        {0.80, 0.80, 350},
		"under_attack":    {0.35, 0.35, 150},
		"kill":            {0.40, 0.40, 90},
		"heat_warning":    {0.30, 0.30, 80},
		"heat_damage":     {0.60, 0.60, 300},
		"died":            {1.00, 1.00, 2000},
		"honk":            {0.50, 0.50, 1200},
		"jet_cone":        {0.80, 0.80, 1500},
		"interdicted":     {0.60, 0.60, 600},
		"escaped":         {0.40, 0.40, 300},
		"hull_breach":     {1.00, 1.00, 1000},
		"limpet":          {0.20, 0.20, 80},
		"docking_granted": {0.15, 0.15, 70},
		"scanned":         {0.25, 0.25, 260},
		"onfoot_shot":     {0, 0.35, 80},
	}
}
