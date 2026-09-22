package haptics

import (
	"math"
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
