package game

import (
	"fmt"
	"log"
	"slices"
	"sort"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/hud"
)

// FireSet is what a trigger fires.
type FireSet struct {
	Classes []string // weapon classes, most first; utility classes if Utility
	Utility bool
}

// FireSets returns what each trigger fires now, by list (hud.Secondary for
// L2, hud.Primary for R2): the settings, else what the HUD showed for this
// fire group, else a guess from the loadout.
func (g *State) FireSets(settings map[string]config.FireGroup) [2]FireSet {
	var sets [2]FireSet
	sets[hud.Primary].Classes, sets[hud.Secondary].Classes = g.guessClasses()
	if lists, ok := g.FireLists[g.fireKey()]; ok {
		for i, l := range lists {
			if l.Known {
				sets[i] = FireSet{Classes: l.Classes, Utility: l.Utility}
			}
		}
	}
	if fg, ok := settings[fmt.Sprint(g.Status.FireGroup+1)]; ok {
		for i, class := range [2]string{hud.Secondary: fg.Secondary, hud.Primary: fg.Primary} {
			if class != "" && class != "auto" {
				sets[i] = FireSet{Classes: []string{class}}
			}
		}
	}
	return sets
}

func (g *State) fireKey() int {
	return hud.FireKey(g.Status.FireGroup, g.Status.Flags.Has(elite.HardpointsDeployed))
}

// guessClasses: the most common weapon class on R2, the next on L2.
func (g *State) guessClasses() (primary, secondary []string) {
	count, biggest := map[string]int{}, map[string]int{}
	var order []string
	for _, m := range g.Modules {
		if m.Utility {
			continue
		}
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

// FiringShare: for a trigger's list, from a fresh HUD read, the part of its
// weapons still firing (1 = all, 0 = all reloading).
func (g *State) FiringShare(list int, now time.Time) float64 {
	read := g.HUD.Lists[list]
	if read.At.IsZero() || now.Sub(read.At) > 1500*time.Millisecond || read.Reloading == 0 {
		return 1
	}
	total := read.Ammo
	if lists, ok := g.FireLists[g.fireKey()]; ok && lists[list].Known {
		total += lists[list].Energy
	}
	if total <= 0 {
		return 1
	}
	return max(0, 1-float64(read.Reloading)/float64(total))
}

// sameNames: the same modules, in any order.
func sameNames(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// LearnFireLists keeps what the HUD showed for each fire group.
func (g *State) LearnFireLists(hs hud.State) {
	for list, l := range hs.Lists {
		if !l.Known {
			continue
		}
		if g.FireLists == nil {
			g.FireLists = map[int][2]hud.FireList{}
		}
		lists := g.FireLists[l.Key]
		old := lists[list]
		lists[list] = l
		g.FireLists[l.Key] = lists
		// logged when the modules change: counts can flicker with a missed entry
		if !sameNames(old.Names, l.Names) {
			hardpoints := "hardpoints out"
			if l.Key%2 == 0 {
				hardpoints = "hardpoints in"
			}
			trigger := [2]string{hud.Secondary: "L2 (secondary)", hud.Primary: "R2 (primary)"}[list]
			log.Printf("Fire group %d, %s: %s fires %s (read from the HUD)", l.Key/2+1, hardpoints, trigger, l)
		}
	}
}
