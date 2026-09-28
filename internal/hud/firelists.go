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
//
// What a fire group's lists hold changes only when the player sets the fire
// groups up again, but entries go missing from the reads for a while: out of
// range, reloading, out of view. So a module, once on a list, stays there
// until it has been missing for a minute of the list in view, and it is
// remembered for when the fire group comes back.

type fireLists struct {
	modules    []elite.Module // the ship's, from the Loadout
	lists      [2]listHistory // reads of the current fire key's lists
	learned    map[int]*[2]learnedList
	key        int // FireKey the reads belong to
	keySet     bool
	keySince   time.Time
	deployedAt time.Time // hardpoints last came out
}

// learnedList is what a fire key's list was judged to hold, and for how
// long, of the time the list was read, each module has been missing.
type learnedList struct {
	names    []nameCount
	missing  map[string]time.Duration
	lastRead time.Time
}

// keepMissing: a module missing from a list for longer is no longer on it.
const keepMissing = time.Minute

type listHistory struct {
	reads         []map[string]int // module names and counts of the last reads
	reloading     int
	ammo          int
	lastReload    time.Time // last read with a reload in it
	reloadStarted time.Time
}

// setModules: the ship's modules, to match list entries against. Elite
// writes the same Loadout again after a fighter or SRV docks and after a
// restock; that keeps what the lists were read to hold. Another ship or a
// refit starts over.
func (p *processor) setModules(mods []elite.Module) {
	same := elite.SameModules(mods, p.fireLists.modules)
	p.fireLists.modules = mods
	if same {
		return
	}
	p.fireLists.lists = [2]listHistory{}
	p.fireLists.learned = nil
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
	l := f.learnedList(list)
	l.read(counts, now)
	h.reads = append(h.reads, counts)
	if len(h.reads) > listReads {
		h.reads = h.reads[1:]
	}
	if len(h.reads) < 3 {
		return
	}
	if names := steadyNames(h.reads, l.names, l.missing); len(names) > 0 {
		l.names = names
		p.state.Lists[list] = describeList(names, f.modules, f.key, *st)
	}
}

// learnedList: what is known of the current fire key's list.
func (f *fireLists) learnedList(list int) *learnedList {
	if f.learned == nil {
		f.learned = map[int]*[2]learnedList{}
	}
	k := f.learned[f.key]
	if k == nil {
		k = &[2]learnedList{}
		f.learned[f.key] = k
	}
	return &k[list]
}

// read counts the time since the last read as missing for the modules on
// the list that this read does not have. A gap longer than a second, the
// list out of view, counts as one second. A module read starts again from
// zero, also one that was dropped and comes back.
func (l *learnedList) read(counts map[string]int, now time.Time) {
	gap := time.Duration(0)
	if !l.lastRead.IsZero() {
		gap = min(now.Sub(l.lastRead), time.Second)
	}
	l.lastRead = now
	if l.missing == nil {
		l.missing = map[string]time.Duration{}
	}
	for n := range counts {
		delete(l.missing, n)
	}
	for _, x := range l.names {
		if counts[x.name] == 0 {
			l.missing[x.name] += gap
		}
	}
}

// listReads: how many of a list's latest reads its contents are judged on.
const listReads = 8

type nameCount struct {
	name  string
	count int
}

// steadyNames: the modules a list holds, with their counts, most entries
// first. A module joins when it is in three quarters of the reads, so an
// entry misread for a moment changes nothing, and once on the list (before)
// it stays until it has been missing for keepMissing.
func steadyNames(reads []map[string]int, before []nameCount, missing map[string]time.Duration) []nameCount {
	seen := map[string][]int{}
	for _, r := range reads {
		for n, c := range r {
			seen[n] = append(seen[n], c)
		}
	}
	var names []nameCount
	for _, x := range before {
		if missing[x.name] < keepMissing {
			names = append(names, nameCount{x.name, listCount(seen[x.name], x.count)})
		}
	}
	need := len(reads) - len(reads)/4
	for n, cs := range seen {
		if len(cs) >= need && !slices.ContainsFunc(names, func(x nameCount) bool { return x.name == n }) {
			names = append(names, nameCount{n, listCount(cs, 0)})
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

// listCount: a module's count on a list from its counts in the reads: the
// highest seen in two reads (entries go missing more often than extra ones
// are misread), else the count shown before.
func listCount(counts []int, before int) int {
	if len(counts) == 0 {
		return before
	}
	freq := map[int]int{}
	for _, c := range counts {
		freq[c]++
	}
	best := 0
	for c, n := range freq {
		if (n >= 2 || c == before) && c > best {
			best = c
		}
	}
	switch {
	case best > 0:
		return best
	case before > 0:
		return before // a read short of entries
	}
	return commonest(counts)
}

// commonest: the most frequent count, the higher on a tie.
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
