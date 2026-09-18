package config

import (
	"regexp"
	"strings"
)

var (
	intArray   = regexp.MustCompile(`\[\s*(-?\d+(?:,\s*-?\d+)*)\s*\]`)
	arraySep   = regexp.MustCompile(`\s*,\s*`)
	smallBlock = regexp.MustCompile(`\{\n(\s*"[^"\n]+": [^{}\n]+\n){1,3}\s*\}`) // innermost {...} of up to three keys
)

// compact puts number arrays and small objects on one line, so the settings
// file reads like a table.
func compact(b []byte) []byte {
	b = intArray.ReplaceAllFunc(b, func(m []byte) []byte {
		inner := intArray.FindSubmatch(m)[1]
		return []byte("[" + strings.Join(arraySep.Split(string(inner), -1), ", ") + "]")
	})
	return smallBlock.ReplaceAllFunc(b, func(m []byte) []byte {
		lines := strings.Split(strings.Trim(string(m), "{}\n "), "\n")
		for i := range lines {
			lines[i] = strings.TrimSpace(lines[i])
		}
		return []byte("{" + strings.Join(lines, " ") + "}")
	})
}
