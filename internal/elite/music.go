package elite

import "strings"

// MainMenuMusic is the Music track while in the main menu.
const MainMenuMusic = "MainMenu"

func IsCombatMusic(track string) bool {
	return strings.HasPrefix(track, "Combat_") || track == "Interdiction"
}

// IsThargoidMusic: the tracks that play near Thargoids.
func IsThargoidMusic(track string) bool {
	switch track {
	case "Unknown_Encounter", "Unknown_Exploration", "Unknown_Settlement", "Combat_Unknown":
		return true
	}
	return false
}
