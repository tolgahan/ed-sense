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
	used := map[string]bool{}
	for name := range effects {
		used[name] = true
	}
	named := regexp.MustCompile(`(?:m\.add|playNow|twice|once)\("([a-z0-9_]+)"|Effect: "([a-z0-9_]+)"|"(fire_primary|fire_secondary)"`)
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
			used[m[1]+m[2]+m[3]] = true
		}
	}
	if len(used) < 80 {
		t.Fatalf("only %d effects found", len(used))
	}
	for name := range used {
		if _, ok := gains[name]; !ok {
			t.Errorf("%s has no haptics_gain entry", name)
		}
	}
}
