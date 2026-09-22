package platform

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/windows/registry"
)

var libraryPath = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// SteamLibraries lists Steam's library folders, Steam's own folder first.
func SteamLibraries() []string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	steam, _, err := k.GetStringValue("SteamPath")
	k.Close()
	if err != nil || steam == "" {
		return nil
	}
	steam = filepath.Clean(steam)
	libs := []string{steam}
	vdf, err := os.ReadFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	for _, m := range libraryPath.FindAllStringSubmatch(string(vdf), -1) {
		if p := filepath.Clean(strings.ReplaceAll(m[1], `\\`, `\`)); !strings.EqualFold(p, steam) {
			libs = append(libs, p)
		}
	}
	return libs
}
