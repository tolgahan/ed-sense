package config

// Default returns the settings a new file starts with.
func Default() Config {
	return Config{
		Version:    Version,
		PollMs:     25,
		Lightbar:   true,
		Triggers:   true,
		PlayerLEDs: true,
		MicLED:     true,
		Brightness: 200,
		Colors:     defaultColors(),
		TriggerFX:  defaultTriggers(),
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
