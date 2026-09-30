package diag

import (
	"log"
	"slices"
	"strings"
	"testing"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/backend/backendtest"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// backends are DS4Windows, whose rumble mutes native haptics, and DSX.
var backends = []struct {
	name     string
	assemble func(backend.Parts) *backend.Backend
}{{"DS4Windows", backend.NewDS4Windows}, {"DSX", backend.NewDSX}}

// recorded puts a backend together around a pad and an audio device that
// record into rec, which takes the log too.
func recorded(assemble func(backend.Parts) *backend.Backend, rec *backendtest.Recorder) (*backend.Backend, *backendtest.Audio) {
	log.SetOutput(rec)
	log.SetFlags(0)
	pad := &backendtest.Pad{Rec: rec, Open: true}
	audio := &backendtest.Audio{Rec: rec, Up: true}
	b := assemble(backend.Parts{Pad: pad, Audio: func(render func(frames []int16)) backend.Audio {
		audio.Render = render
		return audio
	}})
	return b, audio
}

// keepLog returns what puts the log back as it was.
func keepLog() func() {
	out, flags := log.Writer(), log.Flags()
	return func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	}
}

// TestPadTestSides: where rumble mutes native haptics (DS4Windows),
// -padtest plays its sides through native haptics and never rumbles; with
// DSX it rumbles the left side first. No real controller is listed.
func TestPadTestSides(t *testing.T) {
	defer keepLog()()
	oldList := listHID
	listHID = func() []dualsense.HIDDevice { return nil }
	defer func() { listHID = oldList }()
	done := make(chan struct{})
	close(done)
	for _, c := range backends {
		rec := &backendtest.Recorder{}
		b, audio := recorded(c.assemble, rec)
		Rumble(b, done)
		lines := rec.Take()
		var rumble []string
		for _, l := range lines {
			if strings.HasPrefix(l, "pad rumble ") {
				rumble = append(rumble, l)
			}
		}
		if b.Caps.RumbleMutesHaptics {
			if slices.ContainsFunc(rumble, func(l string) bool { return l != "pad rumble 0 0" }) {
				t.Errorf("%s: rumbled: %q", c.name, lines)
			}
			if !slices.Contains(lines, "log Haptics: LEFT side (2 s)") {
				t.Errorf("%s: no left side through native haptics: %q", c.name, lines)
			}
			if got := backendtest.Listen(audio.Render, 480); got != "synth on off" {
				t.Errorf("%s: the left side plays as %s", c.name, got)
			}
			if a, p := slices.Index(lines, "audio close"), slices.Index(lines, "pad close"); a < 0 || a > p {
				t.Errorf("%s: the audio is not closed before the pad: %q", c.name, lines)
			}
			continue
		}
		if len(rumble) == 0 || rumble[0] != "pad rumble 200 0" {
			t.Errorf("%s: rumble %q, want the left side first", c.name, rumble)
		}
		if slices.Contains(lines, "audio maintain") {
			t.Errorf("%s: native haptics opened: %q", c.name, lines)
		}
	}
}

// TestAudioTestsRelease: where rumble mutes native haptics, -hapticstest
// and -feeltest open the virtual DualSense before the audio, since opening
// it switches rumble emulation off, and close it after the audio. With DSX
// they leave the pad alone. No real audio device is listed.
func TestAudioTestsRelease(t *testing.T) {
	defer keepLog()()
	oldList := listAudio
	listAudio = func() []dualsense.AudioDevice { return nil }
	defer func() { listAudio = oldList }()
	done := make(chan struct{})
	close(done)
	for _, test := range []struct {
		name string
		run  func(*backend.Backend)
	}{
		{"-hapticstest", func(b *backend.Backend) { Haptics(b, done) }},
		{"-feeltest", func(b *backend.Backend) { Feel(b, config.Default(), done) }},
	} {
		for _, c := range backends {
			rec := &backendtest.Recorder{}
			b, _ := recorded(c.assemble, rec)
			test.run(b)
			lines := rec.Take()
			open, audio := slices.Index(lines, "pad maintain"), slices.Index(lines, "audio maintain")
			closed, audioClosed := slices.Index(lines, "pad close"), slices.Index(lines, "audio close")
			if audio < 0 || audioClosed < 0 {
				t.Fatalf("%s, %s: no audio: %q", test.name, c.name, lines)
			}
			if !b.Caps.RumbleMutesHaptics {
				if open >= 0 || closed >= 0 {
					t.Errorf("%s, %s: the pad opened: %q", test.name, c.name, lines)
				}
				continue
			}
			if open < 0 || open > audio || closed < audioClosed {
				t.Errorf("%s, %s: the pad is not opened before the audio and closed after it: %q", test.name, c.name, lines)
			}
		}
	}
}
