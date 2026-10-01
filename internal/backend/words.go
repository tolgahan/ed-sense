package backend

import (
	"fmt"
	"strings"

	"github.com/tolgahan/ed-sense/internal/ds4w"
)

// Words is what the player is told about the backend, in the log and in
// messages. DSX's are the words EDSense always had.
type Words struct {
	Name string

	// the loop
	NotAnswering string // once, when it has not answered in the first seconds
	Connected    string
	Lost         string
	HandedBack   string // the controller is back on the player's profile
	OutputsOff   string // an output was switched off while EDSense drives the controller
	NoMotionHint string // no motion data in a minute of flight

	// EDSense's gyro
	NoPadToCalibrate string
	CalibrateNoData  string
	ElevatedLog      string
	ElevatedTell     string
	ProfileKeeps     string // the profile uses its own gyro, so EDSense's stays off
	ProfileUnknown   string // what the profile does with the gyro is unknown
	NoDataLog        string // EDSense's gyro gets no motion data
	NoDataTell       string

	// the demo
	DemoDone       string
	DemoNoAnswer   string
	DemoNoPad      string
	DemoNoFallback string // no native haptics, where rumble would mute them; "": the demo rumbles

	// the devices
	PadMissing     string
	AudioMissing   string
	PadTestMissing string

	// the tray
	TrayOffline string
	PauseTip    string
	OwnGyroTip  string // the "EDSense gyro" item

	// DS4Windows' setup; %s is the detail where there is one
	Warnings map[ds4w.Warning]string
}

// Warning is the text for w, with its detail.
func (w Words) Warning(which ds4w.Warning, detail string) string {
	text := w.Warnings[which]
	if strings.Contains(text, "%s") {
		return fmt.Sprintf(text, detail)
	}
	return text
}

// WordsFor are EDSense's words for kind; DSX's for any kind but
// DS4Windows.
func WordsFor(kind Kind) Words {
	if kind == KindDS4Windows {
		return DS4WindowsWords()
	}
	return DSXWords()
}

// DSXWords are EDSense's words for DSX.
func DSXWords() Words {
	return Words{
		Name:         "DSX",
		NotAnswering: "DSX is not answering. Is DSX running, with Settings > Networking > Incoming UDP on?",
		Connected:    "DSX connected",
		Lost:         "DSX not answering (is DSX running with Incoming UDP on?)",
		HandedBack:   "Controller handed back to your DSX profile",
		NoMotionHint: "Gyro: no motion data from the virtual DualSense in a minute of flight. The turn feel needs DSX's Motion passthrough (Motion page, \"Passthrough\" on)",

		NoPadToCalibrate: "EDSense cannot open DSX's virtual DualSense, so it cannot calibrate the gyro. Is the controller connected in DSX?",
		CalibrateNoData: "No motion data came from DSX's virtual DualSense, so EDSense could not calibrate the gyro.\n\n" +
			".\\EDSense.exe -gyrotest shows more.",
		ElevatedLog: "Gyro: Elite runs as administrator, so EDSense's mouse movement cannot reach it; DSX's gyro aims",
		ElevatedTell: "Elite runs as administrator, so EDSense's gyro cannot reach it and DSX's gyro aims instead.\n\n" +
			"Start Elite (and Steam) normally, or run EDSense as administrator too.",
		ProfileKeeps: "Gyro: the DSX profile for Elite does not use motion to mouse, so EDSense's gyro stays off and the profile's gyro works as set",
		NoDataLog:    "Gyro: no motion data from DSX's virtual DualSense while its motion to mouse is off, so DSX's gyro aims",
		NoDataTell: "EDSense's gyro gets no motion data from DSX's virtual DualSense, so DSX's own gyro aims for now.\n\n" +
			"Untick \"EDSense gyro\" in the tray to keep DSX's gyro. .\\EDSense.exe -gyrotest shows more.",

		DemoDone:     "Demo done, the controller is back on your DSX profile.",
		DemoNoAnswer: "DSX did not answer yet; sending anyway (check Incoming UDP in DSX's settings)",
		DemoNoPad:    "Haptics: no virtual DualSense, the rumble steps will be silent (use DSX's DualSense emulation)",

		PadMissing:     "Haptics: no virtual DualSense found (needs DSX's DualSense emulation)",
		AudioMissing:   "Native haptics: no virtual DualSense audio device (needs DSX's DualSense emulation); using rumble",
		PadTestMissing: "\nNo virtual DualSense found. In DSX, set the controller to DualSense emulation.",

		TrayOffline: "DSX not connected (DSX > Settings > Networking > Incoming UDP)",
		PauseTip:    "Hand the controller back to your DSX profile",
		OwnGyroTip:  "Ticked: EDSense turns the controller's motion into mouse movement. Unticked: DSX does, as before",
	}
}

// DS4WindowsWords are EDSense's words for DS4Windows.
func DS4WindowsWords() Words {
	const gameMods = "Settings > Game mod support (DSX) > \"Let game mods control triggers and lights\""
	const passthru = "the profile's Gyro > Output Mode to Passthru"
	return Words{
		Name:         "DS4Windows",
		NotAnswering: "DS4Windows is not answering. Is DS4Windows running and started, with " + gameMods + " ticked?",
		Connected:    "DS4Windows connected",
		Lost:         "DS4Windows not answering (is it running, with \"Let game mods control triggers and lights\" ticked?)",
		HandedBack:   "Controller handed back to your DS4Windows profile",
		OutputsOff:   "Outputs changed: the controller goes back to your DS4Windows profile, then EDSense sets what it still controls",
		NoMotionHint: "Gyro: no motion data from DS4Windows' virtual DualSense in a minute of flight. The turn feel needs it: set " + passthru,

		NoPadToCalibrate: "EDSense cannot open DS4Windows' virtual DualSense, so it cannot calibrate the gyro. " +
			"Is the controller started in DS4Windows, with a profile that emulates a DualSense?",
		CalibrateNoData: "No motion data came from DS4Windows' virtual DualSense, so EDSense could not calibrate the gyro.\n\n" +
			"Set " + passthru + ". .\\EDSense.exe -gyrotest shows more.",
		ElevatedLog: "Gyro: Elite runs as administrator, so EDSense's mouse movement cannot reach it; EDSense's gyro stays off",
		ElevatedTell: "Elite runs as administrator, so EDSense's gyro cannot reach it.\n\n" +
			"Start Elite (and Steam) normally, or run EDSense as administrator too.",
		ProfileKeeps: "Gyro: the DS4Windows profile uses the gyro itself, so EDSense's gyro stays off and the profile's gyro works as set. " +
			"For EDSense's gyro aim, set " + passthru + ", or Controls with nothing on the gyro",
		ProfileUnknown: "Gyro: EDSense cannot tell what the DS4Windows profile does with the gyro, so EDSense's gyro stays off (the DS4Windows line above says why)",
		NoDataLog:      "Gyro: no motion data from DS4Windows' virtual DualSense, so EDSense's gyro stays off",
		NoDataTell: "EDSense's gyro gets no motion data from DS4Windows' virtual DualSense, so it does not aim.\n\n" +
			"Set " + passthru + ". .\\EDSense.exe -gyrotest shows more.",

		DemoDone:       "Demo done, the controller is back on your DS4Windows profile.",
		DemoNoAnswer:   "DS4Windows did not answer yet; sending anyway (check " + gameMods + ")",
		DemoNoPad:      "Haptics: no virtual DualSense, the rumble steps will be silent (the DS4Windows profile must emulate a DualSense)",
		DemoNoFallback: "Haptics: none, native haptics did not start (rumble through DS4Windows would mute them; haptics_mode \"rumble\" rumbles)",

		PadMissing:     "Haptics: no virtual DualSense found (the DS4Windows profile must emulate a DualSense)",
		AudioMissing:   "Native haptics: no audio device for them (the DS4Windows profile must emulate a DualSense, or the controller be plugged in by USB); no haptics until it is there (rumble would mute them, unless haptics_mode is \"rumble\")",
		PadTestMissing: "\nNo virtual DualSense found. In DS4Windows, edit the profile: Advanced > Emulated Controller > DualSense.",

		TrayOffline: "DS4Windows not connected (Settings > Game mod support (DSX))",
		PauseTip:    "Hand the controller back to your DS4Windows profile",
		OwnGyroTip:  "Ticked: EDSense turns the controller's motion into mouse movement while the DS4Windows profile leaves the gyro alone. Unticked: only the profile's gyro",

		Warnings: map[ds4w.Warning]string{
			ds4w.WarnNotDualSense: "The DS4Windows profile %s does not emulate a DualSense, so EDSense cannot read the controller or play its haptics.\n\n" +
				"In DS4Windows, edit the profile: Advanced > Emulated Controller > DualSense.",
			ds4w.WarnPhysicalVisible: "Games can see your real DualSense next to DS4Windows' virtual one, so Elite may take both as controllers.\n\n" +
				"In DS4Windows, tick Settings > \"Use HidHide to Prevent Double Input\", then plug the controller in again.",
			ds4w.WarnDSXOnPort: "DSX answers on DS4Windows' port, so DS4Windows cannot listen there.\n\n" +
				"Quit DSX (its tray icon > Exit), then press Apply / Retry in DS4Windows under Settings > Game mod support (DSX) > Connection details.",
			ds4w.WarnOldVersion: "DS4Windows %s has no game mod support, so EDSense cannot set the triggers and lights. EDSense needs DS4Windows 5 (see docs\\ds4windows.md).",
			ds4w.WarnTriggerLab: "The DS4Windows profile %s has Trigger Lab on, which wins over EDSense's triggers on that side.\n\n" +
				"Turn Trigger Lab off in the profile to feel EDSense's triggers.",
			ds4w.WarnTouchpadMouse: "The DS4Windows profile %s uses the touchpad as a mouse, so touching it moves the cursor in Elite.\n\n" +
				"In the profile, set Touchpad > Output Mode to Passthru and save it.",
		},
	}
}
