package dsx

import (
	"slices"
	"strings"
)

// TriggerMode is a DSX v3 adaptive trigger mode.
type TriggerMode int

const (
	TriggerOff                       TriggerMode = 20
	TriggerFeedback                  TriggerMode = 21 // start 1-9, strength 1-8
	TriggerWeapon                    TriggerMode = 22 // start 2-7, end 3-8, strength 1-8
	TriggerVibration                 TriggerMode = 23 // start 1-9, amplitude 1-8, frequency 1-40
	TriggerSlopeFeedback             TriggerMode = 24 // start 1-8, end 2-9, start strength 1-8, end strength 1-8
	TriggerMultiplePositionFeedback  TriggerMode = 25 // 10 x strength 0-8
	TriggerMultiplePositionVibration TriggerMode = 26 // frequency 1-40, 10 x amplitude 0-8
)

var triggerModes = map[string]TriggerMode{
	"OFF":                         TriggerOff,
	"FEEDBACK":                    TriggerFeedback,
	"WEAPON":                      TriggerWeapon,
	"VIBRATION":                   TriggerVibration,
	"SLOPE_FEEDBACK":              TriggerSlopeFeedback,
	"MULTIPLE_POSITION_FEEDBACK":  TriggerMultiplePositionFeedback,
	"MULTIPLE_POSITION_VIBRATION": TriggerMultiplePositionVibration,
}

// Trigger is one adaptive trigger effect.
type Trigger struct {
	Mode   TriggerMode
	Params []int
}

func TriggerNone() Trigger { return Trigger{Mode: TriggerOff} }

// NewTrigger builds a trigger from a mode name as the settings file has it;
// an unknown mode is off.
func NewTrigger(mode string, params []int) Trigger {
	m, ok := triggerModes[strings.ToUpper(mode)]
	if !ok {
		return TriggerNone()
	}
	return Trigger{Mode: m, Params: slices.Clone(params)}
}

func (t Trigger) equal(o Trigger) bool {
	return t.Mode == o.Mode && slices.Equal(t.Params, o.Params)
}

// PlayerLEDs is how many of the five player LEDs are lit.
type PlayerLEDs int

const LEDsOff PlayerLEDs = 5

// LitLEDs lights n (1-5) player LEDs.
func LitLEDs(n int) PlayerLEDs { return PlayerLEDs(min(max(n, 1), 5) - 1) }

type MicLED int

const (
	MicOn    MicLED = 0
	MicPulse MicLED = 1
	MicOff   MicLED = 2
)

// Frame is the complete controller output.
type Frame struct {
	Left, Right Trigger
	Lightbar    [3]int // RGB
	Brightness  int    // 0-255
	PlayerLEDs  PlayerLEDs
	Mic         MicLED
}

// DarkFrame: triggers off, lights off.
func DarkFrame() Frame {
	return Frame{Left: TriggerNone(), Right: TriggerNone(), PlayerLEDs: LEDsOff, Mic: MicOff}
}

// Outputs selects what EDSense controls; the rest stays with the DSX profile.
type Outputs struct {
	Triggers, Lightbar, PlayerLEDs, Mic bool
}

// changes lists the instructions for controller idx that turn prev into
// next; with no prev, everything.
func changes(idx int, prev *Frame, next Frame, out Outputs) []instruction {
	var list []instruction
	trigger := func(side int, t Trigger) instruction {
		return instruction{Type: instTriggerUpdate, Parameters: append([]int{idx, side, int(t.Mode)}, t.Params...)}
	}
	if out.Triggers && (prev == nil || !prev.Left.equal(next.Left)) {
		list = append(list, trigger(triggerLeft, next.Left))
	}
	if out.Triggers && (prev == nil || !prev.Right.equal(next.Right)) {
		list = append(list, trigger(triggerRight, next.Right))
	}
	if out.Lightbar && (prev == nil || prev.Lightbar != next.Lightbar || prev.Brightness != next.Brightness) {
		c := next.Lightbar
		list = append(list, instruction{Type: instRGBUpdate, Parameters: []int{idx, c[0], c[1], c[2], next.Brightness}})
	}
	if out.PlayerLEDs && (prev == nil || prev.PlayerLEDs != next.PlayerLEDs) {
		list = append(list, instruction{Type: instPlayerLED, Parameters: []int{idx, int(next.PlayerLEDs)}})
	}
	if out.Mic && (prev == nil || prev.Mic != next.Mic) {
		list = append(list, instruction{Type: instMicLED, Parameters: []int{idx, int(next.Mic)}})
	}
	return list
}
