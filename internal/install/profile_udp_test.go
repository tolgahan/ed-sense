package install

import (
	"slices"
	"testing"

	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
)

// plan4 is a plan with the UDP server's step too.
func plan4(state string, profile, rule, listener, udp string) ds4w.Plan {
	p := plan(state, profile, rule, listener)
	p.Steps = append(p.Steps, ds4w.Step{ID: ds4w.StepUDPServer, State: udp})
	p.UDPEndpoint = "127.0.0.1:26760"
	return p
}

// TestDS4ProfileUDP: the card and its question when DS4Windows' UDP
// server is to be turned on, alone or with game mod support.
func TestDS4ProfileUDP(t *testing.T) {
	const closedIt = "DS4Windows must be closed while EDSense writes it: its tray icon > Exit."
	const step = "Settings > UDP Server > Enable Server on, at 127.0.0.1, for EDSense's gyro aim (Profiles.xml)"

	// a v0.7.0 install: the UDP server alone
	p := plan4(ds4w.PlanPartial, ds4w.StepDone, ds4w.StepDone, ds4w.StepDone, ds4w.StepTodo)
	st := ds4Profile(seen(p), ds4w.Job{}, false)
	plainText(t, st)
	if st.State != control.ProfilePartial || st.Text != `Elite uses EDSense's profile "Elite Dangerous (EDSense)", but DS4Windows' UDP server is off in its settings file, `+
		"which DS4Windows writes when it exits. "+
		"EDSense's gyro aim reads the controller's motion from it, since DS4Windows' virtual DualSense drops every turn under 2 degrees per second. "+
		"EDSense can turn it on." {
		t.Errorf("udp only: %q", st.Text)
	}
	if len(st.Steps) != 4 || st.Steps[3].ID != ds4w.StepUDPServer || st.Steps[3].Text != step || !slices.Equal(st.Files, []string{"Profiles.xml"}) {
		t.Errorf("steps %+v, files %q", st.Steps, st.Files)
	}
	q := st.Install
	if q.Title != "Turn on DS4Windows' UDP server?" || q.OK != "Turn on" ||
		q.Text != `EDSense changes this in %APPDATA%\DS4Windows, and keeps copies of the files in ds4windows_backups next to EDSense:` ||
		!slices.Equal(q.Items, []string{step}) || !slices.Equal(q.Notes, []string{closedIt}) ||
		q.Key != `ds4windows|%APPDATA%\DS4Windows|install|udp_server` {
		t.Errorf("udp only: %+v", q)
	}

	// another PC's address is replaced: the question says so, and its key
	// names it, so an answer to the question without the note is refused
	p.UDPMoves = "192.168.1.5"
	if q := ds4Profile(seen(p), ds4w.Job{}, false).Install; !slices.Equal(q.Notes, []string{closedIt,
		"DS4Windows' UDP server listens on 192.168.1.5 now. EDSense sets it to 127.0.0.1, so programs on other devices can no longer reach it."}) ||
		q.Key != `ds4windows|%APPDATA%\DS4Windows|install|udp_server|moves=192.168.1.5` {
		t.Errorf("address note: %q %q", q.Notes, q.Key)
	}

	// both settings, beside the player's own rule
	p = plan4(ds4w.PlanOther, ds4w.StepSkip, ds4w.StepSkip, ds4w.StepTodo, ds4w.StepTodo)
	p.Rule = &ds4w.Rule{Profile: "My Elite"}
	st = ds4Profile(seen(p), ds4w.Job{}, false)
	if q := st.Install; q.Title != "Turn on DS4Windows' game mod support and UDP server?" || q.OK != "Turn on" || len(q.Items) != 2 ||
		!slices.Equal(st.Files, []string{"Profiles.xml"}) {
		t.Errorf("both, other: %+v, files %q", q, st.Files)
	}
	p.State, p.Rule = ds4w.PlanPartial, nil
	p.Steps[0].State, p.Steps[1].State = ds4w.StepDone, ds4w.StepDone
	if st := ds4Profile(seen(p), ds4w.Job{}, false); st.Text != `Elite uses EDSense's profile "Elite Dangerous (EDSense)", but DS4Windows' game mod support and UDP server are off: `+
		"EDSense sends the triggers and lights through the first, and reads the gyro from the second. EDSense can turn them on." {
		t.Errorf("both: %q", st.Text)
	}

	// everything to do: one Profiles.xml in the files, four items
	p = plan4(ds4w.PlanMissing, ds4w.StepTodo, ds4w.StepTodo, ds4w.StepTodo, ds4w.StepTodo)
	st = ds4Profile(seen(p), ds4w.Job{}, false)
	if !slices.Equal(st.Files, []string{`Profiles\Elite Dangerous (EDSense).xml`, "Auto Profiles.xml", "Profiles.xml"}) ||
		len(st.Install.Items) != 4 || st.Install.Title != "Install the DS4Windows profile for Elite?" {
		t.Errorf("all: files %q, %+v", st.Files, st.Install)
	}
	waiting := ds4Profile(seen(p), ds4w.Job{State: ds4w.JobWaiting, Dir: testDS4Dir, Steps: p.Todo(false)}, false)
	if !slices.Equal(waiting.Files, st.Files) {
		t.Errorf("waiting: files %q", waiting.Files)
	}

	// done: the card and the message name what was turned on
	for _, c := range []struct {
		wrote      []string
		text, tell string
	}{
		{[]string{ds4w.StepListener}, "Written. Start DS4Windows again: its game mod support is on.",
			"DS4Windows' game mod support is turned on. Start DS4Windows again to use it."},
		{[]string{ds4w.StepUDPServer}, "Written. Start DS4Windows again: its UDP server is on.",
			"DS4Windows' UDP server is turned on. Start DS4Windows again to use it."},
		{[]string{ds4w.StepListener, ds4w.StepUDPServer}, "Written. Start DS4Windows again: its game mod support and UDP server are on.",
			"DS4Windows' game mod support and UDP server are turned on. Start DS4Windows again to use it."},
		{[]string{ds4w.StepProfile, ds4w.StepRule, ds4w.StepListener, ds4w.StepUDPServer},
			`Written. Start DS4Windows again: Elite gets the "Elite Dangerous (EDSense)" profile whenever it is in front.`,
			"The DS4Windows profile for Elite is written. Start DS4Windows again to use it."},
	} {
		r := ds4w.Result{Wrote: c.wrote}
		if got := doneText(r); got != c.text {
			t.Errorf("%v: %q", c.wrote, got)
		}
		if got := doneMessage(r); got != c.tell {
			t.Errorf("%v: %q", c.wrote, got)
		}
	}
	if got := wroteBefore(ds4w.Result{Wrote: []string{ds4w.StepProfile, ds4w.StepListener, ds4w.StepUDPServer}}); got !=
		`Written before that, and left as written: Profiles\Elite Dangerous (EDSense).xml, Profiles.xml.` {
		t.Errorf("wrote before: %q", got)
	}
	for _, c := range []struct {
		todo          []string
		listener, udp bool
	}{
		{[]string{ds4w.StepListener}, true, false},
		{[]string{ds4w.StepUDPServer}, false, true},
		{[]string{ds4w.StepListener, ds4w.StepUDPServer}, true, true},
		{[]string{ds4w.StepRule, ds4w.StepUDPServer}, false, false},
		{nil, false, false},
	} {
		if l, u := settingsOnly(c.todo); l != c.listener || u != c.udp {
			t.Errorf("%v: %v %v", c.todo, l, u)
		}
	}
}
