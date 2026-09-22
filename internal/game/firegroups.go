package game

import (
	"fmt"
	"sort"

	"github.com/tolgahan/ed-sense/internal/config"
)

// The fire group lists by trigger: L2 fires the SECONDARY list, R2 the
// PRIMARY one.
const (
	Secondary = 0 // L2
	Primary   = 1 // R2
)

// FireSet is what a trigger fires.
type FireSet struct {
	Classes []string // weapon classes, most first
}

// FireSets returns what each trigger fires now, by list (Secondary for L2,
// Primary for R2): the settings, else a guess from the loadout.
func (g *State) FireSets(settings map[string]config.FireGroup) [2]FireSet {
	var sets [2]FireSet
	sets[Primary].Classes, sets[Secondary].Classes = g.guessClasses()
	if fg, ok := settings[fmt.Sprint(g.Status.FireGroup+1)]; ok {
		for i, class := range [2]string{Secondary: fg.Secondary, Primary: fg.Primary} {
			if class != "" && class != "auto" {
				sets[i] = FireSet{Classes: []string{class}}
			}
		}
	}
	return sets
}

// guessClasses: the most common weapon class on R2, the next on L2.
func (g *State) guessClasses() (primary, secondary []string) {
	count, biggest := map[string]int{}, map[string]int{}
	var order []string
	for _, m := range g.Modules {
		if count[m.Class] == 0 {
			order = append(order, m.Class)
		}
		count[m.Class]++
		biggest[m.Class] = max(biggest[m.Class], m.Size)
	}
	switch len(order) {
	case 0:
		return []string{"generic"}, []string{"generic"}
	case 1:
		return []string{order[0]}, []string{order[0]}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if count[a] != count[b] {
			return count[a] > count[b]
		}
		return biggest[a] > biggest[b]
	})
	return []string{order[0]}, []string{order[1]}
}
