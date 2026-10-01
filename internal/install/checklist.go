// Package install checks how the controller apps are set up for EDSense
// (the setup checklists the window shows), and writes EDSense's profiles
// for Elite into them: the Service.
package install

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/control"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
)

// Inputs are what a checklist is built from, gathered off the loop.
type Inputs struct {
	App    string            // control.AppDSX or control.AppDS4Windows: the app checked
	Status control.Status    // the core's status; Kind is the app EDSense uses now
	Seen   control.Detection // which apps run, and who answers where
	Report *ds4w.Report      // DS4Windows' profile checks while EDSense uses it; nil: none yet
	Pads   Pads
	// Profile is the app's profile card, nil when not known: DSX's
	// profile item, and what an install can fix.
	Profile *control.ProfileState
	Plan    *ds4w.Plan // DS4Windows' files as an install sees them; nil: not read
}

// Pads is what the HID devices show.
type Pads struct {
	Virtual  bool // the app's virtual DualSense is there
	Physical bool // games can see a real DualSense
}

// Checklist is the setup checklist of in.App: one Item per check, always
// the same ones in the same order.
func Checklist(in Inputs) []control.Item {
	if in.App == control.AppDS4Windows {
		return ds4windowsChecks(in)
	}
	return dsxChecks(in)
}

// Item ids.
const (
	IDApp        = "app"         // the app runs (DS4Windows: version 5)
	IDListener   = "listener"    // it answers where EDSense sends
	IDController = "controller"  // it lists a controller
	IDVirtualPad = "virtual_pad" // its virtual DualSense is there
	IDProfile    = "profile"     // DS4Windows: the profile emulates a DualSense
	IDGyro       = "gyro"        // DS4Windows: the profile leaves the gyro to EDSense
	IDTouchpad   = "touchpad"    // DS4Windows: the touchpad is not a mouse
	IDTriggerLab = "trigger_lab" // DS4Windows: Trigger Lab is off
	IDHidHide    = "hidhide"     // DS4Windows: games see only the virtual DualSense
	IDDSXProfile = "dsx_profile" // DSX: its "Elite Dangerous" profile, used for Elite
)

func item(id, state, text string) control.Item { return control.Item{ID: id, State: state, Text: text} }

func dsxChecks(in Inputs) []control.Item {
	seen := in.Seen.DSX
	uses := in.Status.Kind == control.AppDSX
	var items []control.Item

	app := item(IDApp, control.ItemOK, "DSX runs")
	if !seen.Running {
		app = item(IDApp, control.ItemBad, "DSX is not running")
		app.How = "Start DSX. EDSense sends the triggers and lights to it."
	}
	items = append(items, app)

	listener := item(IDListener, control.ItemBad, fmt.Sprintf("DSX does not answer on %s", seen.Addr))
	switch {
	case seen.Answers == control.AppDSX:
		listener = item(IDListener, control.ItemOK, fmt.Sprintf("DSX answers on %s", seen.Addr))
	case seen.Answers == control.AppDS4Windows:
		listener.Text = fmt.Sprintf("DS4Windows answers on DSX's port (%s)", seen.Addr)
		listener.How = "Only one app can listen there. Quit DS4Windows (its tray icon > Exit), or give one of the two another port."
	case !seen.Running:
		listener = item(IDListener, control.ItemWait, "Checked once DSX runs")
	default:
		listener.How = "In DSX, turn on Settings > Networking > Incoming UDP."
	}
	items = append(items, listener)

	items = append(items, controller(in, "DSX", uses, "Connect the controller, and check that DSX shows it."))

	pad := item(IDVirtualPad, control.ItemOK, "The virtual DualSense is there")
	switch {
	case in.Pads.Virtual:
	case !seen.Running:
		pad = item(IDVirtualPad, control.ItemWait, "Checked once DSX runs")
	default:
		pad = item(IDVirtualPad, control.ItemBad, "No virtual DualSense")
		pad.How = "In DSX, set the controller to DualSense emulation. EDSense reads the controller and plays the haptics through it."
	}
	items = append(items, pad)
	return append(items, dsxProfileItem(in.Profile))
}

// dsxProfileItem is DSX's "Elite Dangerous" profile, as its card has it.
func dsxProfileItem(ps *control.ProfileState) control.Item {
	it := item(IDDSXProfile, control.ItemUnknown, "Checking DSX's profiles")
	if ps == nil {
		return it
	}
	name := strconv.Quote(dsx.ProfileName)
	switch ps.State {
	case control.ProfilePresent:
		it = item(IDDSXProfile, control.ItemOK, "DSX has an "+name+" profile, set for Elite")
	case control.ProfileMissing:
		it = item(IDDSXProfile, control.ItemWarn, "DSX has no "+name+" profile")
		it.How = "EDSense comes with one, with gyro aim, the touchpad and the triggers set up for Elite. Add it on the DSX profile card."
	case control.ProfileNotForElite:
		it = item(IDDSXProfile, control.ItemWarn, "DSX picks no profile when Elite starts")
		it.How = "Reset on the DSX profile card puts EDSense's " + name + " profile back, and has DSX use it for Elite."
		if ps.Player != "" {
			// the player's own pick for Elite, which EDSense leaves alone
			it = item(IDDSXProfile, control.ItemOK, fmt.Sprintf("Elite gets your DSX profile %q", ps.Player))
			it.How = "EDSense leaves the profile you picked for Elite in DSX as it is. It needs at least DualSense Emulation as its virtual device."
		}
	case control.ProfileWaiting:
		it = item(IDDSXProfile, control.ItemWait, "EDSense writes its DSX profile once DSX is closed (DSX tray icon > Exit)")
	case control.ProfileWriting:
		it = item(IDDSXProfile, control.ItemWait, "EDSense is writing its DSX profile")
	case control.ProfileDone:
		it = item(IDDSXProfile, control.ItemWait, "Start DSX again to use EDSense's profile")
	case control.ProfileFailed:
		it = item(IDDSXProfile, control.ItemBad, "The DSX profile could not be written")
		it.How = ps.Text
	case control.ProfileNoFolder:
		it = item(IDDSXProfile, control.ItemUnknown, "DSX's folder was not found")
		it.How = "Start DSX, so EDSense finds its folder."
	}
	if it.State != control.ItemOK && (ps.CanInstall || ps.CanReset) {
		it.Fix = control.FixInstall
	}
	return it
}

func ds4windowsChecks(in Inputs) []control.Item {
	w := backend.DS4WindowsWords()
	seen := in.Seen.DS4Windows
	uses := in.Status.Kind == control.AppDS4Windows
	var items []control.Item

	app := item(IDApp, control.ItemOK, "DS4Windows runs")
	switch {
	case !seen.Running:
		app = item(IDApp, control.ItemBad, "DS4Windows is not running")
		app.How = "Start DS4Windows 5, and press Start in it if the controller is stopped."
		app.Link = "ds4windows_doc"
	// the Report's warning is from when the session started: the version
	// seen now wins (an update since), and it is used only without one
	case oldVersion(seen.Version) || seen.Version == "" && has(in.Report, ds4w.WarnOldVersion):
		app = item(IDApp, control.ItemBad, "This DS4Windows has no game mod support")
		if seen.Version != "" {
			app.Text = fmt.Sprintf("DS4Windows %s has no game mod support", seen.Version)
		}
		app.How = "EDSense needs DS4Windows 5."
		app.Link = "ds4windows_releases"
	case seen.Version != "":
		app.Text = fmt.Sprintf("DS4Windows %s runs", seen.Version)
	}
	items = append(items, app)

	listener := item(IDListener, control.ItemBad, fmt.Sprintf("Game mod support does not answer on %s", seen.Addr))
	switch {
	case seen.Answers == control.AppDS4Windows:
		listener = item(IDListener, control.ItemOK, fmt.Sprintf("Game mod support answers on %s", seen.Addr))
	case seen.Answers == control.AppDSX:
		listener.Text, listener.How = split(w.Warning(ds4w.WarnDSXOnPort, ""))
	case !seen.Running:
		listener = item(IDListener, control.ItemWait, "Checked once DS4Windows runs")
	default:
		listener.How = "In DS4Windows, tick Settings > Game mod support (DSX) > \"Let game mods control triggers and lights\"."
		listener.Link = "ds4windows_doc"
		listener.Fix = fixes(in.Profile, ds4w.StepListener)
	}
	items = append(items, listener)

	items = append(items, controller(in, "DS4Windows", uses,
		"Connect the DualSense by USB or Bluetooth, and press Start in DS4Windows if it is stopped."))

	pad := item(IDVirtualPad, control.ItemOK, "The virtual DualSense is there")
	switch {
	case in.Pads.Virtual:
	case !seen.Running:
		pad = item(IDVirtualPad, control.ItemWait, "Checked once DS4Windows runs")
	default:
		pad = item(IDVirtualPad, control.ItemBad, "No virtual DualSense")
		pad.How = "In DS4Windows, edit the profile: Advanced > Emulated Controller > DualSense."
	}
	items = append(items, pad)

	r := in.Report
	profile := fromProfile(in, uses, IDProfile)
	if profile.State == "" {
		profile = item(IDProfile, control.ItemOK, fmt.Sprintf("The profile %q emulates a DualSense", r.Profile))
		if !ds4w.EmulatesDualSense(r.Output) {
			profile.State = control.ItemBad
			profile.Text, profile.How = split(w.Warning(ds4w.WarnNotDualSense, fmt.Sprintf("%q (%s)", r.Profile, r.Output)))
		}
	}
	items = append(items, profile)

	gyro := fromProfile(in, uses, IDGyro)
	const passthru = "For EDSense's gyro aim, set the profile's Gyro > Output Mode to Passthru."
	switch {
	case gyro.State == control.ItemLater:
	case !in.Status.Gyro.Aim:
		gyro = item(IDGyro, control.ItemOK, "Gyro aim is off")
	case in.Status.Gyro.By != "" && in.Status.Gyro.By != config.GyroByEDSense:
		gyro = item(IDGyro, control.ItemOK, "The profile's gyro aims: EDSense gyro is off")
	case gyro.State != "":
	case r.Gyro == ds4w.GyroFree:
		gyro = item(IDGyro, control.ItemOK, fmt.Sprintf("The profile %q leaves the gyro free, so EDSense aims", r.Profile))
	case r.Gyro == ds4w.GyroMouse:
		gyro = item(IDGyro, control.ItemWarn, fmt.Sprintf("The profile %q uses the gyro as a mouse, so EDSense's gyro stays off", r.Profile))
		gyro.How = passthru
	case r.Gyro == ds4w.GyroOther:
		gyro = item(IDGyro, control.ItemWarn, fmt.Sprintf("The profile %q uses the gyro itself, so EDSense's gyro stays off", r.Profile))
		gyro.How = passthru
	default:
		gyro = item(IDGyro, control.ItemUnknown, "EDSense cannot tell what the profile does with the gyro")
		gyro.How = passthru
	}
	items = append(items, gyro)

	touch := fromProfile(in, uses, IDTouchpad)
	if touch.State == "" {
		touch = item(IDTouchpad, control.ItemOK, "The touchpad is not a mouse")
		if has(r, ds4w.WarnTouchpadMouse) {
			touch.State = control.ItemWarn
			touch.Text, touch.How = split(w.Warning(ds4w.WarnTouchpadMouse, strconv.Quote(r.Profile)))
		}
	}
	items = append(items, touch)

	lab := fromProfile(in, uses, IDTriggerLab)
	if lab.State == "" {
		lab = item(IDTriggerLab, control.ItemOK, "Trigger Lab is off")
		if has(r, ds4w.WarnTriggerLab) {
			lab.State = control.ItemWarn
			lab.Text, lab.How = split(w.Warning(ds4w.WarnTriggerLab, strconv.Quote(r.Profile)))
		}
	}
	items = append(items, lab)

	// what EDSense's profile changes for Elite
	for i, it := range items {
		switch it.ID {
		case IDProfile, IDGyro, IDTouchpad, IDTriggerLab:
			if it.State == control.ItemWarn || it.State == control.ItemBad {
				items[i].Fix = fixes(in.Profile, ds4w.StepProfile, ds4w.StepRule)
			}
		}
	}

	// HidHide is the player's to set up; DS4Windows' own option is only a
	// hint, since HidHide can be set up without it
	hide := item(IDHidHide, control.ItemOK, "Games see only the virtual DualSense")
	switch {
	case !uses:
		hide = later(IDHidHide, "DS4Windows")
	case in.Pads.Physical:
		hide.State = control.ItemBad
		hide.Text, hide.How = split(w.Warning(ds4w.WarnPhysicalVisible, ""))
		if in.Plan != nil && in.Plan.Exclusive {
			hide.How = "DS4Windows' \"Use HidHide to Prevent Double Input\" is on, yet games still see the controller. " +
				"Check in HidHide that it hides the DualSense, then plug the controller in again."
		}
	case !in.Status.Full || !in.Status.Online || in.Status.Controllers == 0:
		hide = item(IDHidHide, control.ItemUnknown, "Connect the controller to check this")
	}
	return append(items, hide)
}

// fixes is FixInstall when ps offers an install with one of the steps to
// do, else "".
func fixes(ps *control.ProfileState, steps ...string) string {
	if ps == nil || !ps.CanInstall {
		return ""
	}
	for _, s := range ps.Steps {
		if s.State == ds4w.StepTodo && slices.Contains(steps, s.ID) {
			return control.FixInstall
		}
	}
	return ""
}

// controller is the item for the controllers name lists, which only the
// backend in use knows.
func controller(in Inputs, name string, uses bool, how string) control.Item {
	st := in.Status
	switch {
	case !uses:
		return later(IDController, name)
	case !st.Full:
		return item(IDController, control.ItemUnknown, "Not known yet")
	case !st.Online:
		return item(IDController, control.ItemWait, "Checked once "+name+" answers")
	case st.Controllers == 1:
		return item(IDController, control.ItemOK, name+" lists the controller")
	case st.Controllers > 1:
		return item(IDController, control.ItemOK, fmt.Sprintf("%s lists %d controllers", name, st.Controllers))
	}
	it := item(IDController, control.ItemBad, name+" lists no controller")
	it.How = how
	return it
}

// fromProfile is the item for a check of the DS4Windows profile while it
// cannot be made; State "" when it can.
func fromProfile(in Inputs, uses bool, id string) control.Item {
	r := in.Report
	switch {
	case !uses:
		return later(id, "DS4Windows")
	case r == nil:
		return item(id, control.ItemWait, "Checking the DS4Windows profile")
	case r.Profile == "":
		if id == IDProfile {
			return item(id, control.ItemUnknown, "EDSense could not tell which DS4Windows profile is in use")
		}
		return item(id, control.ItemUnknown, "Not known without the profile")
	case r.Problem != "":
		if id == IDProfile {
			return item(id, control.ItemUnknown, fmt.Sprintf("The profile %q could not be read", r.Profile))
		}
		return item(id, control.ItemUnknown, "Not known without the profile")
	}
	return control.Item{ID: id}
}

func later(id, name string) control.Item {
	return item(id, control.ItemLater, "Checked once EDSense uses "+name)
}

// has: the checks of r found w.
func has(r *ds4w.Report, w ds4w.Warning) bool {
	if r == nil {
		return false
	}
	for _, f := range r.Warnings {
		if f == w {
			return true
		}
	}
	return false
}

// oldVersion: version is known and below 5.
func oldVersion(version string) bool {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n < 5
}

// split is a message as an item's text and how: its first paragraph says
// what, the rest what to do.
func split(msg string) (text, how string) {
	text, how, _ = strings.Cut(msg, "\n\n")
	return strings.TrimSuffix(text, "."), strings.ReplaceAll(how, "\n\n", " ")
}
