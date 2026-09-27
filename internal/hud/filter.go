package hud

// valueFilter turns per-frame reads of a number into a steady value. The
// glyph reader misreads now and then (a hit flash can cover a digit), so a
// new value is only taken when it is close to the current one or has been
// read the same way a few times in a row.
type valueFilter struct {
	near     int // a drop by up to this much is taken at once
	need     int // reads in a row to take any other value
	max      int // the highest value the HUD shows
	minScore float64

	value   int
	ok      bool
	pending int
	count   int
}

// feed takes a read and returns the steady value, and whether one is known.
func (f *valueFilter) feed(n Number) (int, bool) {
	v, ok := n.Value()
	if !ok || n.Score < f.minScore || v > f.max {
		return f.value, f.ok
	}
	// small drops are taken at once (hits), rises only one step at a time
	// (shields refill slowly)
	if f.ok && v <= f.value && f.value-v <= f.near || f.ok && v == f.value+1 {
		f.value, f.pending, f.count = v, 0, 0
		return f.value, true
	}
	if f.count > 0 && v == f.pending {
		f.count++
	} else {
		f.pending, f.count = v, 1
	}
	need := f.need
	if len(n.Text) == 2 && f.ok && f.value >= 15 {
		need += 3 // one digit read: often the first one was hidden
	}
	if f.count >= need {
		f.value, f.ok, f.count = v, true, 0
	}
	return f.value, f.ok
}

func (f *valueFilter) reset() { f.ok, f.count = false, 0 }
