package launch

import (
	"strconv"
	"strings"
)

// The WebView2 Runtime's registry keys, one per channel (stable, beta,
// dev, canary), as its loader looks them up.
const runtimeKeys = `Software\Microsoft\EdgeUpdate\ClientState\`

var runtimeChannels = []struct{ name, guid string }{
	{"", "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"},
	{"beta", "{2CD8A007-E189-409D-A2C8-9AF4EF3C72AA}"},
	{"dev", "{0D50BFEC-CD6A-4F9A-964C-C7416E3ACB10}"},
	{"canary", "{65C35B14-6C1D-4122-AC46-7148CC9D6497}"},
}

// minRuntime is the oldest runtime the loader accepts.
var minRuntime = [4]int{86, 0, 616, 0}

// runtimeVersion reads a runtime version such as "129.0.2792.65"; the
// zero version, which an uninstalled runtime can leave behind, is not one.
func runtimeVersion(s string) ([4]int, bool) {
	var v [4]int
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, v != [4]int{}
}

// usableRuntime: the version in a runtime folder's name is recent enough.
func usableRuntime(version string) bool {
	v, ok := runtimeVersion(version)
	if !ok {
		return false
	}
	for i := range v {
		if v[i] != minRuntime[i] {
			return v[i] > minRuntime[i]
		}
	}
	return true
}
