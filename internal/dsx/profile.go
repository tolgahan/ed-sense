package dsx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

func profilePath(dsxDir string) string { return controllerProfilePath(dsxDir, ProfileName) }

func controllerProfilePath(dsxDir, name string) string {
	return filepath.Join(dsxDir, "DSX_Savefile", "Configuration Files", "Controller Profiles", name+".dsx")
}

func gameProfilesPath(dsxDir string) string {
	return filepath.Join(dsxDir, "DSX_Savefile", "Configuration Files", "Game Profiles", "GameProfilesUpdates.json")
}

// ProfileFiles are the files the Installer writes, as named in DSX's
// folder.
var ProfileFiles = []string{
	`DSX_Savefile\Configuration Files\Controller Profiles\` + ProfileName + ".dsx",
	`DSX_Savefile\Configuration Files\Game Profiles\GameProfilesUpdates.json`,
}

// GyroReader is the DSX backend's setup work, on the loop: it finds DSX's
// folder and reads what the controller profile DSX uses for Elite does
// with the gyro. It writes nothing: the Installer adds EDSense's profile,
// off the loop.
type GyroReader struct {
	running func() bool
	find    func(running bool) string

	dsxFolder           string
	gyroRead            bool // the profile's gyro mode was read once
	gyroMouse, gyroKnow bool
}

// NewGyroReader reads the DSX that runs, or the one in a Steam library.
func NewGyroReader() *GyroReader { return newGyroReader(dsxRunning, findDSXFolder) }

func newGyroReader(running func() bool, find func(running bool) string) *GyroReader {
	return &GyroReader{running: running, find: find}
}

func dsxRunning() bool { return platform.ProcessRunning(dsxExe) }

// GyroToMouse: whether the DSX profile Elite uses has its gyro on motion
// to mouse. Known is false when that can't be read (no DSX folder, no
// profile picked for Elite). It is read again at every Step, since DSX
// saves its profiles when it exits.
func (g *GyroReader) GyroToMouse() (yes, known bool) {
	if !g.gyroRead {
		if g.dsxFolder == "" {
			g.dsxFolder = g.find(g.running())
		}
		g.readGyro()
	}
	return g.gyroMouse, g.gyroKnow
}

// Step finds DSX's folder and reads the gyro again; call it every few
// seconds.
func (g *GyroReader) Step() {
	if d := g.find(g.running()); d != "" {
		g.dsxFolder = d
	}
	g.readGyro()
}

func (g *GyroReader) readGyro() {
	g.gyroRead = true
	g.gyroMouse, g.gyroKnow = profileGyro(g.dsxFolder)
}

// Profiles tells, for -gyrotest, which controller profile DSX applies when
// Elite starts and which one it used last (its profile_usage.json), so
// probably the one on now. Either is "" when it can't be read.
func Profiles() (forElite, inUse string) {
	dir := findDSXFolder(platform.ProcessRunning(dsxExe))
	if dir == "" {
		return "", ""
	}
	return eliteProfile(dir), profileInUse(dir)
}

// eliteProfile: the controller profile DSX's game profile for Elite names,
// "" when there is none (DSX then keeps whatever profile is on).
func eliteProfile(dsxDir string) string {
	b, err := os.ReadFile(gameProfilesPath(dsxDir))
	if err != nil {
		return ""
	}
	var games map[string]struct{ ProfileName string }
	if json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\ufeff")), &games) != nil {
		return ""
	}
	return games[elite.SteamAppID].ProfileName
}

// profileInUse: the profile DSX used last, by its own usage record.
func profileInUse(dsxDir string) string {
	b, err := os.ReadFile(filepath.Join(dsxDir, "DSX_Savefile", "Configuration Files", "Controller Profiles", "profile_usage.json"))
	if err != nil {
		return ""
	}
	var usage []struct {
		Name string    `json:"profile_name"`
		Last time.Time `json:"last_used_at"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\ufeff")), &usage) != nil {
		return ""
	}
	var name string
	var last time.Time
	for _, u := range usage {
		if u.Name != "" && u.Last.After(last) {
			name, last = u.Name, u.Last
		}
	}
	return name
}

// profileGyro reads which controller profile DSX applies when Elite starts,
// and whether that profile's gyro is motion to mouse.
func profileGyro(dsxDir string) (mouse, known bool) {
	if dsxDir == "" {
		return false, false
	}
	name := eliteProfile(dsxDir)
	if name == "" {
		return false, false
	}
	b, err := os.ReadFile(controllerProfilePath(dsxDir, name))
	if err != nil {
		return false, false
	}
	var prof struct {
		Motion struct {
			Mode string `json:"motion_mode"`
		} `json:"controller_motion"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\ufeff")), &prof) != nil || prof.Motion.Mode == "" {
		return false, false
	}
	return prof.Motion.Mode == "MOTION_TO_MOUSE", true
}

// installProfile writes the bundled profile. With force it replaces the
// player's, keeping a copy in backupDir; without, it never replaces one. A
// profile that cannot be read is left as it is. It also points DSX's game
// profile for Elite at it: always with force, else only if Elite has
// none. It returns what it did, for the log ("" when the profile was
// there). now names the copies.
func installProfile(dsxDir, backupDir, eliteExe string, force bool, now time.Time) (string, error) {
	dst := profilePath(dsxDir)
	var did []string
	switch old, err := os.ReadFile(dst); {
	case err == nil:
		if !force {
			return "", nil
		}
		backup, err := keepCopy(backupDir, ProfileName+"-"+now.Format(stamp), ".dsx", old)
		if err != nil {
			return "", fmt.Errorf("backup: %w", err)
		}
		did = append(did, "old profile saved as "+backup)
	case !errors.Is(err, fs.ErrNotExist):
		// no copy can be kept of it, and it may be the player's
		return "", fmt.Errorf("the profile there could not be read, so it was left as it is: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := writeFile(dst, assets.DSXProfile, force); errors.Is(err, errThere) {
		return "", nil // DSX saved one meanwhile
	} else if err != nil {
		return "", err
	}
	did = append(did, "profile written to "+dst)
	switch msg, err := useForElite(dsxDir, backupDir, eliteExe, force, now); {
	case err != nil:
		did = append(did, "game profile not set: "+err.Error())
	case msg != "":
		did = append(did, msg)
	}
	return strings.Join(did, "; "), nil
}

// stamp names the copies by the time they were made.
const stamp = "20060102-150405"

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// useForElite makes DSX apply the profile when Elite starts. DSX's file is
// keyed by Steam app ID, with a BOM and CRLF line ends. Its numbers are
// kept as written, and a copy of it goes to backupDir before it changes.
func useForElite(dsxDir, backupDir, eliteExe string, force bool, now time.Time) (string, error) {
	path := gameProfilesPath(dsxDir)
	entries := map[string]map[string]any{}
	raw, err := os.ReadFile(path)
	bom := false
	switch {
	case err == nil:
		b := raw
		if bytes.HasPrefix(b, utf8BOM) {
			b, bom = b[len(utf8BOM):], true
		}
		if len(bytes.TrimSpace(b)) > 0 {
			if err := decodeNumbers(b, &entries); err != nil {
				return "", fmt.Errorf("unreadable %s: %w", filepath.Base(path), err)
			}
			if entries == nil {
				return "", fmt.Errorf("unreadable %s: it holds null", filepath.Base(path))
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
	case entry == nil:
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
	var did []string
	if raw != nil {
		backup, err := keepCopy(backupDir, "GameProfilesUpdates-"+now.Format(stamp), ".json", raw)
		if err != nil {
			return "", fmt.Errorf("backup of %s: %w", filepath.Base(path), err)
		}
		did = append(did, "game profiles saved as "+backup)
	}
	text := strings.ReplaceAll(string(out), "\n", "\r\n")
	if bom {
		text = "\ufeff" + text
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := writeFile(path, []byte(text), true); err != nil {
		return "", err
	}
	return strings.Join(append(did, "DSX will use it when Elite Dangerous starts"), "; "), nil
}

// decodeNumbers reads one JSON value from b into v, keeping numbers as
// written (DSX may keep .NET ticks, which a float64 would round).
func decodeNumbers(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("more than one value")
	}
	return nil
}

// keepCopy writes b as name+ext in dir, or name-2+ext and so on when a
// copy of that name is there, and returns its path. The copy is flushed
// to the disk before the file it copies is replaced.
func keepCopy(dir, name, ext string, b []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for n := 1; ; n++ {
		path := filepath.Join(dir, name+ext)
		if n > 1 {
			path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", name, n, ext))
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) && n < 100 {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(b)
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(path)
			return "", err
		}
		return path, nil
	}
}

// errThere: the file writeFile was not to replace is there.
var errThere = errors.New("the file is there")

// writeFile replaces path with b through a temporary file in the same
// folder, so a file cut short is never left in its place. Without
// replace, a file that is there right before the rename stays (errThere).
func writeFile(path string, b []byte, replace bool) error {
	f, err := os.CreateTemp(filepath.Dir(path), "edsense-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && !replace {
		if _, serr := os.Lstat(path); !errors.Is(serr, fs.ErrNotExist) {
			err = errThere
		}
	}
	if err == nil {
		for i := 0; ; i++ {
			// a virus scanner or an indexer may hold the file for a moment
			if err = os.Rename(tmp, path); err == nil || i == 4 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
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

// isDSXFolder: dir holds DSX's saved settings.
func isDSXFolder(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "DSX_Savefile", "Configuration Files"))
	return err == nil && st.IsDir()
}

// newestDSXFolder: of the candidates, the one whose save folder was used last.
func newestDSXFolder(candidates []string) string {
	type folder struct {
		dir  string
		used time.Time
	}
	var found []folder
	for _, d := range candidates {
		if st, err := os.Stat(filepath.Join(d, "DSX_Savefile", "Configuration Files")); err == nil && st.IsDir() {
			found = append(found, folder{d, st.ModTime()})
		}
	}
	if len(found) == 0 {
		return ""
	}
	sort.Slice(found, func(i, k int) bool { return found[i].used.After(found[k].used) })
	return found[0].dir
}

// EliteExes are the Elite exes EDSense knows: the running game's, then
// Odyssey's and Horizons' in each Steam library.
func EliteExes() []string {
	var exes []string
	add := func(p string) {
		if !slices.ContainsFunc(exes, func(e string) bool { return strings.EqualFold(e, p) }) {
			exes = append(exes, p)
		}
	}
	if exe := platform.ProcessPath(elite.GameExe); exe != "" {
		add(exe)
	}
	for _, lib := range platform.SteamLibraries() {
		for _, product := range []string{"elite-dangerous-odyssey-64", "elite-dangerous-64"} {
			p := filepath.Join(lib, "steamapps", "common", "Elite Dangerous", "Products", product, elite.GameExe)
			if exists(p) {
				add(p)
			}
		}
	}
	return exes
}

// findEliteExe: the running game, else Odyssey or Horizons in a Steam library.
func findEliteExe() string {
	if exes := EliteExes(); len(exes) > 0 {
		return exes[0]
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
