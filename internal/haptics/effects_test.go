package haptics

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/config"
)

// Every effect the code plays can be tuned in the settings file.
func TestEveryEffectHasAGain(t *testing.T) {
	gains := config.Default().HapticsGain
	rumble := config.Default().Rumble
	used := map[string]bool{}
	for _, name := range journalFeels {
		used[name] = true
	}
	named := regexp.MustCompile(`(?:m\.add|play|playNow|twice)\("([a-z0-9_]+)"|Effect: "([a-z0-9_]+)"`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range named.FindAllStringSubmatch(string(src), -1) {
			used[m[1]+m[2]] = true
		}
	}
	if len(used) < 40 {
		t.Fatalf("only %d effects found", len(used))
	}
	for name := range used {
		if _, ok := gains[name]; !ok {
			t.Errorf("%s has no haptics_gain entry", name)
		}
		if _, ok := rumble[name]; !ok {
			t.Errorf("%s has no rumble entry", name)
		}
	}
}
