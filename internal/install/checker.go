package install

import (
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// Checker gathers a checklist's inputs, off the loop: listing the HID
// devices and probing the apps take a moment.
type Checker struct {
	Status  func() control.Status
	Detect  func(fresh bool) control.Detection // kept a while by its own cache
	Report  func() *ds4w.Report                // the session's DS4Windows checks; nil: none
	Recheck func()                             // has the session check DS4Windows' profile again; nil: none
	List    func() []dualsense.HIDDevice       // nil: backend.HIDDevices
	Now     func() time.Time                   // nil: time.Now
	// Profile is the app's profile card, and DS4Windows' files as an
	// install sees them (Service.Look); nil: not known
	Profile func(app string) (control.ProfileState, *ds4w.Plan)

	mu        sync.Mutex
	devs      []dualsense.HIDDevice
	listed    time.Time
	rechecked time.Time
}

// How long the HID devices listed are kept: a fresh check lists them
// again once they are a second old.
const (
	listKeep  = 2 * time.Second
	listFresh = time.Second
)

// The DS4Windows profile is checked again for each of the window's polls
// (every 2 s), at most once in recheckKeep unless fresh, and a Check
// waits up to reportWait for that check.
const recheckKeep = 1500 * time.Millisecond

var reportWait = time.Second

// Check is app's checklist now. fresh looks again at what is kept for a
// while: the apps and the HID devices. While EDSense uses DS4Windows, its
// profile is checked again, and the checklist shows that check.
func (c *Checker) Check(app string, fresh bool) control.Setup {
	st := c.Status()
	uses := st.Kind == app
	var before *ds4w.Report
	var until time.Time // the check asked for is waited for until then
	if uses && app == control.AppDS4Windows && c.Recheck != nil && c.Report != nil && c.recheckDue(fresh) {
		before = c.Report()
		c.Recheck()
		until = time.Now().Add(reportWait)
	}
	in := Inputs{App: app, Status: st, Seen: c.Detect(fresh)}
	devs := c.devices(fresh)
	if uses && c.Report != nil {
		in.Report = c.Report()
		if !until.IsZero() {
			in.Report = c.newReport(before, until)
		}
	}
	_, in.Pads.Virtual = backend.VirtualIn(devs, backend.Kind(app))
	in.Pads.Physical = backend.PhysicalIn(devs)
	if c.Profile != nil {
		ps, plan := c.Profile(app)
		in.Profile, in.Plan = &ps, plan
	}
	return control.Setup{App: app, T: c.now().UnixMilli(), Items: Checklist(in)}
}

// recheckDue: the profile may be checked again now.
func (c *Checker) recheckDue(fresh bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if !fresh && !c.rechecked.IsZero() && now.Sub(c.rechecked) < recheckKeep {
		return false
	}
	c.rechecked = now
	return true
}

// newReport is the Report of the check asked for while before was the
// last one, waited for until then; else the last one.
func (c *Checker) newReport(before *ds4w.Report, until time.Time) *ds4w.Report {
	for {
		r := c.Report()
		if r != before || !time.Now().Before(until) {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// devices are the HID devices, as listed at most listKeep ago.
func (c *Checker) devices(fresh bool) []dualsense.HIDDevice {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	age := now.Sub(c.listed)
	if c.listed.IsZero() || age >= listKeep || fresh && age >= listFresh {
		list := c.List
		if list == nil {
			list = backend.HIDDevices
		}
		c.devs, c.listed = list(), now
	}
	return c.devs
}
