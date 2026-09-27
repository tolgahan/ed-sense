package vision

// Component is a connected region of a mask.
type Component struct {
	ID         int32 // label in the label image, from 1
	X, Y, W, H int   // bounding box
	Area       int   // pixel count
	CX, CY     float64
}

// FillAtLeast reports whether the component covers at least frac of its
// bounding box.
func (c Component) FillAtLeast(frac float64) bool { return float64(c.Area) >= frac*float64(c.W*c.H) }

// Components labels the 8-connected regions of mask (w x h). Components are
// numbered in raster order of their first pixel; labels holds each pixel's
// component ID, 0 for background.
func Components(mask []bool, w, h int) (labels []int32, comps []Component) {
	labels = make([]int32, w*h)
	parent := []int32{0}
	find := func(a int32) int32 {
		for parent[a] != a {
			parent[a] = parent[parent[a]]
			a = parent[a]
		}
		return a
	}
	next := int32(1)
	for y := range h {
		for x := range w {
			if !mask[y*w+x] {
				continue
			}
			var nb [4]int32
			n := 0
			if x > 0 && labels[y*w+x-1] != 0 {
				nb[n] = labels[y*w+x-1]
				n++
			}
			if y > 0 {
				for dx := -1; dx <= 1; dx++ {
					if xx := x + dx; xx >= 0 && xx < w && labels[(y-1)*w+xx] != 0 {
						nb[n] = labels[(y-1)*w+xx]
						n++
					}
				}
			}
			if n == 0 {
				labels[y*w+x] = next
				parent = append(parent, next)
				next++
				continue
			}
			root := find(nb[0])
			for i := 1; i < n; i++ {
				root = min(root, find(nb[i]))
			}
			labels[y*w+x] = root
			for i := range n {
				if r := find(nb[i]); r != root {
					parent[r] = root
				}
			}
		}
	}
	// second pass: final IDs and boxes (W, H hold the far corner until the end)
	ids := make([]int32, len(parent))
	for y := range h {
		for x := range w {
			l := labels[y*w+x]
			if l == 0 {
				continue
			}
			r := find(l)
			id := ids[r]
			if id == 0 {
				comps = append(comps, Component{ID: int32(len(comps) + 1), X: x, Y: y, W: x, H: y})
				id = int32(len(comps))
				ids[r] = id
			}
			labels[y*w+x] = id
			c := &comps[id-1]
			c.X, c.Y = min(c.X, x), min(c.Y, y)
			c.W, c.H = max(c.W, x), max(c.H, y)
			c.Area++
		}
	}
	for i := range comps {
		c := &comps[i]
		c.W, c.H = c.W-c.X+1, c.H-c.Y+1
		c.CX, c.CY = float64(c.X)+float64(c.W)/2, float64(c.Y)+float64(c.H)/2
	}
	return labels, comps
}

// dilate grows a mask by one pixel in the four directions.
func dilate(m []bool, w, h int) []bool {
	out := append([]bool(nil), m...)
	for y := range h {
		for x := range w {
			if !m[y*w+x] {
				continue
			}
			if y > 0 {
				out[(y-1)*w+x] = true
			}
			if y < h-1 {
				out[(y+1)*w+x] = true
			}
			if x > 0 {
				out[y*w+x-1] = true
			}
			if x < w-1 {
				out[y*w+x+1] = true
			}
		}
	}
	return out
}
