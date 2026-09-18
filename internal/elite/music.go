package elite

import "strings"

// MainMenuMusic is the Music track while in the main menu.
const MainMenuMusic = "MainMenu"

func IsCombatMusic(track string) bool {
	return strings.HasPrefix(track, "Combat_") || track == "Interdiction"
}
