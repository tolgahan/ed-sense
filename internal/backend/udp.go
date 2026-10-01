package backend

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsu"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// DS4Windows' UDP server (Settings > UDP Server), the motion server
// emulators such as Cemu read: the motion of DS4Windows' controllers 1 to
// 4 without the virtual DualSense's dead band. EDSense asks it on this PC
// only, where DS4Windows' Profiles.xml has it listen. DS4Windows writes
// that file when it exits, so a server moved to 127.0.0.1 in its window
// is still asked there.

// DS4WindowsUDPBiasFile keeps the gyro calibration for the motion of
// DS4Windows' UDP server: the virtual DualSense's dead band hides the
// small drift it shows.
const DS4WindowsUDPBiasFile = "gyro_calibration_ds4windows_udp.json"

// udpEvery is how often DS4Windows' UDP server's settings are looked at
// again; tests make it short.
var udpEvery = 3 * time.Second

// ds4wUDP is DS4Windows' UDP server as EDSense reads it: the client, and
// what DS4Windows' settings say of the server, read again when
// Profiles.xml changes.
type ds4wUDP struct {
	client  *dsu.Client
	dataDir func() string

	mu   sync.Mutex
	info udpInfo

	// follow's own
	dir  string    // the data folder read
	seen fileStamp // its Profiles.xml as last read
}

// fileStamp tells whether a file changed.
type fileStamp struct {
	mod  time.Time
	size int64 // -1: no file
}

func stampOf(dir string) fileStamp {
	if dir == "" {
		return fileStamp{size: -1}
	}
	fi, err := os.Stat(filepath.Join(dir, "Profiles.xml"))
	if err != nil {
		return fileStamp{size: -1}
	}
	return fileStamp{fi.ModTime(), fi.Size()}
}

func (a fileStamp) same(b fileStamp) bool { return a.mod.Equal(b.mod) && a.size == b.size }

// udpSettings are DS4Windows' settings in dir; found is false when none
// were read, and its defaults apply.
func udpSettings(dir string) (ds4w.Settings, bool) {
	if dir == "" {
		return ds4w.Settings{}, false
	}
	st, err := ds4w.ReadSettings(dir)
	if err != nil {
		return ds4w.Settings{}, false
	}
	return st, true
}

// udpElsewhere is the log line for a UDP server EDSense does not read:
// where the settings have it, and where EDSense asks all the same.
const udpElsewhere = "DS4Windows: its UDP server listens on %q, which EDSense does not use (it reads it on this PC only); " +
	"the gyro's motion comes from the virtual DualSense, and EDSense asks %s in case the address is set to 127.0.0.1 in DS4Windows"

// newDS4WUDP starts reading DS4Windows' UDP server where its settings have
// it, for the controller the DSX listener lists first, while the virtual
// DualSense is open; nil when it cannot.
func newDS4WUDP(client *dsx.Client, link *dualsense.Link) *ds4wUDP {
	u := &ds4wUDP{dataDir: ds4wDataDir}
	u.dir = u.dataDir()
	u.seen = stampOf(u.dir)
	st, found := udpSettings(u.dir)
	addr, ok, shown := st.UDPEndpoint()
	u.info = udpInfo{elsewhere: !ok, addr: shown, smoothed: st.UDPSmoothing}
	want := func() dsu.Want {
		w := dsu.Want{Active: link.Available(), Slot: -1}
		if c := client.Controllers(); client.Online() && len(c) > 0 {
			w.Slot = c[0]
			w.MAC, _ = dsu.ParseMAC(client.MAC(c[0]))
		}
		return w
	}
	c, err := dsu.New(dsu.Options{Addr: addr, Want: want})
	if err != nil {
		log.Printf("DS4Windows: cannot read its UDP server: %v", err)
		return nil
	}
	u.client = c
	switch {
	case !ok:
		log.Printf(udpElsewhere, shown, addr)
	case !found:
		log.Printf("DS4Windows: gyro motion from its UDP server on %s when it sends (%s)", addr, DS4WindowsSettingsFrom(""))
	default:
		on := "off"
		if st.UDPServer {
			on = "on"
		}
		log.Printf("DS4Windows: gyro motion from its UDP server on %s when it sends (%s, where it is %s)", addr, DS4WindowsSettingsFrom(u.dir), on)
	}
	return u
}

// Info is what DS4Windows' settings say of its UDP server now.
func (u *ds4wUDP) Info() udpInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.info
}

// follow checks DS4Windows' settings every so often, until stop is closed.
func (u *ds4wUDP) follow(every time.Duration, stop <-chan struct{}) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		u.check()
	}
}

// check reads Profiles.xml again when it changed, and asks the UDP server
// where it has it now. While the server does not answer, the data folder
// is looked up again, since a portable DS4Windows keeps its own.
func (u *ds4wUDP) check() {
	if !u.client.State().Answers {
		if d := u.dataDir(); d != u.dir {
			u.dir, u.seen = d, fileStamp{size: -2} // read it whatever it holds
		}
	}
	now := stampOf(u.dir)
	if now.same(u.seen) {
		return
	}
	u.seen = now
	st, found := udpSettings(u.dir)
	addr, ok, shown := st.UDPEndpoint()
	in := udpInfo{elsewhere: !ok, addr: shown, smoothed: st.UDPSmoothing}
	u.mu.Lock()
	was := u.info
	u.info = in
	u.mu.Unlock()
	from := ""
	if found {
		from = u.dir
	}
	changed, err := u.client.Retarget(addr)
	same := !changed && in.elsewhere == was.elsewhere && (!in.elsewhere || in.addr == was.addr)
	switch {
	case err != nil:
		log.Printf("DS4Windows: cannot reach its UDP server on %s: %v", addr, err)
	case same:
	case ok:
		log.Printf("DS4Windows: its UDP server is on %s now (%s)", addr, DS4WindowsSettingsFrom(from))
	default:
		log.Printf(udpElsewhere, shown, addr)
	}
}
