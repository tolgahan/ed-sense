package elite

import (
	"os"
	"path/filepath"
)

const GameExe = "EliteDangerous64.exe"

// JournalDir: %USERPROFILE%\Saved Games\Frontier Developments\Elite Dangerous
func JournalDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Saved Games", "Frontier Developments", "Elite Dangerous")
}
