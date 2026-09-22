package dsx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/assets"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/platform"
)

// ProfileName is the DSX controller profile EDSense brings for Elite.
const ProfileName = "Elite Dangerous"

const dsxExe = "DSX.exe"

func profilePath(dsxDir string) string {
	return filepath.Join(dsxDir, "DSX_Savefile", "Configuration Files", "Controller Profiles", ProfileName+".dsx")
}

func gameProfilesPath(dsxDir string) string {
	return filepath.Join(dsxDir, "DSX_Savefile", "Configuration Files", "Game Profiles", "GameProfilesUpdates.json")
}

// ProfileInstaller adds EDSense's DSX profile the first time, when DSX has
// no "Elite Dangerous" profile, and replaces it on request. DSX keeps its
// profiles in memory and saves them when it exits, so files are written
// only while DSX is closed.
type ProfileInstaller struct {
	backupDir string
	notify    func(string) // tells the player (a message box)

	checked   bool
	pending   bool
	force     bool
	toldWait  bool
	dsxFolder string
}

func NewProfileInstaller(backupDir string, notify func(string)) *ProfileInstaller {
	return &ProfileInstaller{backupDir: backupDir, notify: notify}
}

// RequestReset replaces DSX's "Elite Dangerous" profile with the bundled one.
func (p *ProfileInstaller) RequestReset() {
	p.checked, p.pending, p.force, p.toldWait = true, true, true, false
}

// Step does the work when it can; call it every few seconds.
func (p *ProfileInstaller) Step() {
	running := platform.ProcessRunning(dsxExe)
	if d := findDSXFolder(running); d != "" {
		p.dsxFolder = d
	}
	if !p.checked {
		p.checked = true
		p.firstCheck(running)
	}
	if !p.pending {
		return
	}
	if running {
		if !p.toldWait {
			log.Print("DSX profile: waiting for DSX to be closed")
			p.toldWait = true
		}
		return
	}
	p.pending, p.toldWait = false, false
	if p.dsxFolder == "" {
		p.notify("DSX's folder was not found, so the profile could not be written.")
		return
	}
	did, err := installProfile(p.dsxFolder, p.backupDir, findEliteExe(), p.force)
	p.force = false
	switch {
	case err != nil:
		log.Printf("DSX profile not written: %v", err)
		p.notify("The DSX profile could not be written:\n" + err.Error())
	case did != "":
		log.Printf("DSX profile %q: %s", ProfileName, did)
		p.notify(fmt.Sprintf("The %q controller profile is now in DSX. Start DSX again to use it.", ProfileName))
	}
}

func (p *ProfileInstaller) firstCheck(running bool) {
	switch {
	case p.dsxFolder == "":
		log.Print("DSX folder not found: EDSense's Elite controller profile was not added (tray > Reset DSX profile tries again)")
	case !exists(profilePath(p.dsxFolder)):
		p.pending = true
		log.Printf("DSX has no %q profile yet: EDSense adds its own when DSX is closed", ProfileName)
		if running {
			p.notify("EDSense comes with a DSX controller profile for Elite Dangerous (gyro aim, touchpad and trigger setup).\n\n" +
				"It will be added the next time DSX is closed (DSX tray icon > Exit). Then start DSX again.")
			p.toldWait = true
		}
	}
}

// installProfile writes the bundled profile. With force it replaces the
// player's, keeping a copy in backupDir. It also points DSX's game profile
// for Elite at it: always with force, else only if Elite has none. It
// returns what it did, for the log.
func installProfile(dsxDir, backupDir, eliteExe string, force bool) (string, error) {
	dst := profilePath(dsxDir)
	var did []string
	if old, err := os.ReadFile(dst); err == nil {
		if !force {
			return "", nil
		}
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return "", err
		}
		backup := filepath.Join(backupDir, fmt.Sprintf("%s-%s.dsx", ProfileName, time.Now().Format("20060102-150405")))
		if err := os.WriteFile(backup, old, 0o644); err != nil {
			return "", fmt.Errorf("backup: %w", err)
		}
		did = append(did, "old profile saved as "+backup)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, assets.DSXProfile, 0o644); err != nil {
		return "", err
	}
	did = append(did, "profile written to "+dst)
	switch msg, err := useForElite(dsxDir, eliteExe, force); {
	case err != nil:
		did = append(did, "game profile not set: "+err.Error())
	case msg != "":
		did = append(did, msg)
	}
	return strings.Join(did, "; "), nil
}

// useForElite makes DSX apply the profile when Elite starts. DSX's file is
// keyed by Steam app ID, with a BOM and CRLF line ends.
func useForElite(dsxDir, eliteExe string, force bool) (string, error) {
	path := gameProfilesPath(dsxDir)
	entries := map[string]map[string]any{}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
		if len(strings.TrimSpace(string(b))) > 0 {
			if err := json.Unmarshal(b, &entries); err != nil {
				return "", fmt.Errorf("unreadable %s: %w", filepath.Base(path), err)
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", err
	}
	entry, has := entries[elite.SteamAppID]
	switch {
	case has && !force:
		return "", nil
	case !has && eliteExe == "":
		return "", errors.New("no Elite Dangerous install found; pick the profile in DSX > Installed Games")
	case !has:
		entry = map[string]any{}
	}
	if cur, _ := entry["ExePath"].(string); cur == "" && eliteExe != "" {
		entry["ExePath"] = eliteExe
	}
	entry["ProfileName"] = ProfileName
	entries[elite.SteamAppID] = entry
	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(out), "\n", "\r\n")), 0o644); err != nil {
		return "", err
	}
	return "DSX will use it when Elite Dangerous starts", nil
}

// findDSXFolder: the running DSX's folder, else DSX in a Steam library.
func findDSXFolder(running bool) string {
	if running {
		if exe := platform.ProcessPath(dsxExe); exe != "" {
			if d := newestDSXFolder([]string{filepath.Dir(exe)}); d != "" {
				return d
			}
		}
	}
	var candidates []string
	for _, lib := range platform.SteamLibraries() {
		dirs, _ := filepath.Glob(filepath.Join(lib, "steamapps", "common", "DSX", "*"))
		candidates = append(candidates, dirs...)
	}
	return newestDSXFolder(candidates)
}

// newestDSXFolder: of the candidates, the one whose save folder was used last.
func newestDSXFolder(candidates []string) string {
	type folder struct {
		dir  string
		used time.Time
	}
	var found []folder
	for _, d := range candidates {
		st, err := os.Stat(filepath.Join(d, "DSX_Savefile", "Configuration Files"))
		if err == nil && st.IsDir() {
			found = append(found, folder{d, st.ModTime()})
		}
	}
	if len(found) == 0 {
		return ""
	}
	sort.Slice(found, func(i, k int) bool { return found[i].used.After(found[k].used) })
	return found[0].dir
}

// findEliteExe: the running game, else Odyssey or Horizons in a Steam library.
func findEliteExe() string {
	if exe := platform.ProcessPath(elite.GameExe); exe != "" {
		return exe
	}
	for _, lib := range platform.SteamLibraries() {
		for _, product := range []string{"elite-dangerous-odyssey-64", "elite-dangerous-64"} {
			p := filepath.Join(lib, "steamapps", "common", "Elite Dangerous", "Products", product, elite.GameExe)
			if exists(p) {
				return p
			}
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
