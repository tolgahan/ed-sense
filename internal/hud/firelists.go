package hud

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
)

// Fire groups from the HUD: what the lists show becomes what R2 and L2 fire,
// so the trigger feel matches the weapons whatever the fire group setup, and
// a weapon's ammo line reading RELOADING becomes a reload feel.

type fireLists struct {
	modules    []elite.Module // the ship's, from the Loadout
	lists      [2]listHistory
	key        int // FireKey the reads belong to
	keySet     bool
	keySince   time.Time
	deployedAt time.Time // hardpoints last came out
}

type listHistory struct {
	reads         []map[string]int // module names and counts of the last reads
	reloading     int
	ammo          int
	lastReload    time.Time // last read with a reload in it
	reloadStarted time.Time
}

// setModules: the ship's modules, to match list entries against.
func (p *processor) setModules(mods []elite.Module) {
	p.fireLists.modules = mods
	p.fireLists.lists = [2]listHistory{}
	p.state.Lists = [2]FireList{}
}

// setFireKey: the fire group or the hardpoints changed; list reads start over.
func (p *processor) setFireKey(key int, deployed bool, now time.Time) {
	f := &p.fireLists
	if key == f.key && f.keySet {
		return
	}
	if deployed && (!f.keySet || f.key%2 == 0) {
		f.deployedAt = now
	}
	f.key, f.keySet, f.keySince = key, true, now
	for i := range f.lists {
		f.lists[i].reads = nil
		p.state.Lists[i].Known = false
	}
}

// feedList takes one read of a list.
func (p *processor) feedList(list int, rd ListRead, now time.Time) {
	f := &p.fireLists
	if !rd.OK || len(f.modules) == 0 {
		return
	}
	h := &f.lists[list]
	counts := map[string]int{}
	ammo, reloading := 0, 0
	for _, e := range rd.Entries {
		i, _ := MatchEntry(e, f.modules)
		if i < 0 {
			continue
		}
		m := f.modules[i]
		counts[m.Name]++
		if m.Ammo && !m.Utility && !e.Red {
			ammo++
			// RELOADING with the clip bar empty; DEPLOYING looks the same for a
			// moment after the hardpoints come out
			if e.Sub == StatusLine && e.Clip < 0.1 && now.Sub(f.deployedAt) > 3*time.Second {
				reloading++
			}
		}
	}
	if len(counts) == 0 {
		return // not in view
	}
	st := &p.state.Lists[list]
	st.At, st.Ammo, st.Reloading = now, ammo, reloading
	switch {
	case reloading > h.reloading && now.Sub(h.reloadStarted) > 800*time.Millisecond:
		p.emit(Event{Kind: ReloadStart, Strength: 1, Side: ListSide(list)})
		h.reloadStarted = now
	case h.reloading > 0 && reloading == 0 && ammo >= h.ammo:
		// done only with as many weapons in view as before: a list half out
		// of view does not end a reload
		p.emit(Event{Kind: ReloadDone, Strength: 1, Side: ListSide(list)})
	}
	switch {
	case reloading > 0:
		h.reloading, h.ammo, h.lastReload = reloading, ammo, now
	case ammo >= h.ammo || now.Sub(h.lastReload) > 6*time.Second:
		h.reloading, h.ammo = 0, ammo
	}

	if now.Sub(f.keySince) < 1200*time.Millisecond {
		return // the lists are still changing
	}
	h.reads = append(h.reads, counts)
	if len(h.reads) > listReads {
		h.reads = h.reads[1:]
	}
	if len(h.reads) < 3 {
		return
	}
	var shown []string
	if st.Known && st.Key == f.key {
		shown = st.Names
	}
	if names := steadyNames(h.reads, shown); len(names) > 0 {
		p.state.Lists[list] = describeList(names, f.modules, f.key, *st)
	}
}

// listReads: how many of a list's latest reads its contents are judged on.
const listReads = 8

type nameCount struct {
	name  string
	count int
}

// steadyNames: the modules a list holds, with their most common count, most
// entries first. A module joins when it is in three quarters of the reads
// and, once shown, stays until it is missing from three quarters of them, so
// an entry misread or out of view for a moment changes nothing.
func steadyNames(reads []map[string]int, shown []string) []nameCount {
	seen := map[string][]int{}
	for _, r := range reads {
		for n, c := range r {
			seen[n] = append(seen[n], c)
		}
	}
	var names []nameCount
	for n, cs := range seen {
		need := len(reads) - len(reads)/4
		if slices.Contains(shown, n) {
			need = (len(reads) + 3) / 4
		}
		if len(cs) >= need {
			names = append(names, nameCount{n, commonest(cs)})
		}
	}
	sort.Slice(names, func(a, b int) bool {
		if names[a].count != names[b].count {
			return names[a].count > names[b].count
		}
		return names[a].name < names[b].name
	})
	return names
}

// commonest: the most frequent count, the higher on a tie (entries go out of
// view more often than extra ones are misread).
func commonest(counts []int) int {
	freq := map[int]int{}
	best := 0
	for _, c := range counts {
		freq[c]++
		if freq[c] > freq[best] || freq[c] == freq[best] && c > best {
			best = c
		}
	}
	return best
}

// describeList fills in what a list fires; st holds its latest read.
func describeList(names []nameCount, mods []elite.Module, key int, st FireList) FireList {
	byName := map[string]elite.Module{}
	for _, m := range mods {
		byName[m.Name] = m
	}
	st.Known, st.Key = true, key
	st.Names, st.Counts, st.Classes, st.Utility, st.Energy = nil, nil, nil, false, 0
	weapons, utilities := map[string]int{}, map[string]int{}
	var weaponOrder, utilityOrder []string
	for _, x := range names {
		st.Names = append(st.Names, x.name)
		st.Counts = append(st.Counts, x.count)
		m := byName[x.name]
		if m.Utility {
			if utilities[m.Class] == 0 {
				utilityOrder = append(utilityOrder, m.Class)
			}
			utilities[m.Class] += x.count
			continue
		}
		if weapons[m.Class] == 0 {
			weaponOrder = append(weaponOrder, m.Class)
		}
		weapons[m.Class] += x.count
		if !m.Ammo {
			st.Energy += x.count
		}
	}
	order, counts := weaponOrder, weapons
	if len(weaponOrder) == 0 {
		order, counts, st.Utility = utilityOrder, utilities, true
	}
	sort.SliceStable(order, func(a, b int) bool { return counts[order[a]] > counts[order[b]] })
	st.Classes = order
	return st
}

// String: "MULTI-CANNON x3, HEATSINK x2".
func (l FireList) String() string {
	parts := make([]string, len(l.Names))
	for i, n := range l.Names {
		parts[i] = n
		if l.Counts[i] > 1 {
			parts[i] = fmt.Sprintf("%s x%d", n, l.Counts[i])
		}
	}
	return strings.Join(parts, ", ")
}
