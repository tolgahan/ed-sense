package elite

import (
	"os"
	"path/filepath"
)

const (
	GameExe    = "EliteDangerous64.exe"
	SteamAppID = "359320"
)

// JournalDir: %USERPROFILE%\Saved Games\Frontier Developments\Elite Dangerous
func JournalDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Saved Games", "Frontier Developments", "Elite Dangerous")
}

// OptionsDir: %LOCALAPPDATA%\Frontier Developments\Elite Dangerous\Options
func OptionsDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	return filepath.Join(local, "Frontier Developments", "Elite Dangerous", "Options")
}

// BindingsDir holds the control presets.
func BindingsDir() string {
	if d := OptionsDir(); d != "" {
		return filepath.Join(d, "Bindings")
	}
	return ""
}

// GraphicsOverrideFile holds the HUD colour matrix, if the player set one.
func GraphicsOverrideFile() string {
	if d := OptionsDir(); d != "" {
		return filepath.Join(d, "Graphics", "GraphicsConfigurationOverride.xml")
	}
	return ""
}
