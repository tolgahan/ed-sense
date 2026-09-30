package haptics

import (
	"math"
	"slices"
	"testing"

	"github.com/tolgahan/ed-sense/internal/dualsense"
)

func rms(buf []int16, channel int) float64 {
	var sum float64
	n := len(buf) / 4
	for i := range n {
		v := float64(buf[i*4+channel])
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

func TestSynthChannels(t *testing.T) {
	s := NewSynth()
	s.SetLayer("x", Voice{Wave: Sine, F0: 60, Amp: 0.8, L: 1}, 1)
	buf := make([]int16, 4800*4) // 100 ms
	s.Render(buf)
	if rms(buf, 0) != 0 || rms(buf, 1) != 0 {
		t.Fatal("the speaker channels stay silent")
	}
	if rms(buf, 2) < 5000 || rms(buf, 3) != 0 {
		t.Fatalf("left actuator only: L %.0f R %.0f", rms(buf, 2), rms(buf, 3))
	}
	s.SetLayer("x", Voice{Wave: Sine, F0: 60, Amp: 0.8, L: 1}, 0)
	s.Render(buf)
	s.Render(buf)
	if rms(buf, 2) > 50 {
		t.Fatalf("the layer fades out, rms %.0f", rms(buf, 2))
	}
	s.Play(effects["hull_hit"], 1)
	s.Render(buf)
	if rms(buf, 2) < 3000 || rms(buf, 3) < 3000 {
		t.Fatal("a hull hit on both sides")
	}
	for range 10 {
		s.Render(buf)
	}
	if rms(buf, 2) != 0 || len(s.shots) != 0 {
		t.Fatal("the one-shot ends")
	}
	for name, voices := range effects {
		s.Play(voices, 1)
		s.Render(buf)
		for _, v := range buf {
			if v == math.MinInt16 {
				t.Fatalf("%s overflows", name)
			}
		}
	}
}

func TestNormalisedNoise(t *testing.T) {
	s := NewSynth()
	for _, cutoff := range []float64{40, 150, 900} {
		s.shots = nil
		s.Play([]Voice{{Wave: NormNoise, F0: cutoff, Amp: 1, L: 1, Hold: 1}}, 1)
		buf := make([]int16, 4*dualsense.SampleRate)
		s.Render(buf)
		if v := rms(buf, 2) / 32000; v < 0.35 || v > 0.6 {
			t.Errorf("%v Hz: rms %.2f, want about 0.5", cutoff, v)
		}
	}
}

// The same layers render the same samples whatever order they were set in,
// also with two noise voices sharing the generator, and a layer that faded
// out leaves the order.
func TestSynthDeterministic(t *testing.T) {
	voices := map[string]Voice{
		"rattle": {Wave: Noise, F0: 300, Amp: 0.6, L: 1, R: 0.5},
		"rumble": {Wave: NormNoise, F0: 60, Amp: 0.5, L: 0.5, R: 1},
		"hum":    {Wave: Sine, F0: 170, Amp: 0.4, L: 1, R: 1},
	}
	render := func(keys []string) []int16 {
		s := NewSynth()
		for _, k := range keys {
			s.SetLayer(k, voices[k], 0.8)
		}
		out := make([]int16, 4800*4)
		for range 5 {
			s.Render(out)
		}
		return out
	}
	a := render([]string{"rattle", "rumble", "hum"})
	for _, keys := range [][]string{{"hum", "rumble", "rattle"}, {"rumble", "hum", "rattle"}} {
		if b := render(keys); !slices.Equal(a, b) {
			t.Fatalf("layers set in the order %v render other samples", keys)
		}
	}

	s := NewSynth()
	for _, k := range []string{"rumble", "hum", "rattle"} {
		s.SetLayer(k, voices[k], 1)
	}
	s.SetLayer("hum", voices["hum"], 0)
	s.Render(make([]int16, 48000*4)) // a second: the hum fades out and goes
	var keys []string
	for _, l := range s.order {
		keys = append(keys, l.key)
	}
	if !slices.Equal(keys, []string{"rattle", "rumble"}) || len(s.layers) != 2 {
		t.Errorf("after the hum faded: order %v, %d layers", keys, len(s.layers))
	}
	s.SetLayer("aa", voices["hum"], 1)
	if s.order[0].key != "aa" || len(s.order) != 3 {
		t.Errorf("a new layer is not in key order: first %q of %d", s.order[0].key, len(s.order))
	}
}
