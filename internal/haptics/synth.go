package haptics

import (
	"math"
	"sync"

	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// The DualSense (and DSX's virtual one) is also a 4-channel audio device:
// channels 3 and 4 drive the left and right haptic actuators. Synth renders
// waveforms for them, so every effect gets its own frequency, texture and
// envelope, and the sides are independent.

type Wave int

const (
	Sine Wave = iota
	Square
	Triangle
	Saw
	Noise     // low-passed white noise, F0 = cutoff; quieter as the cutoff drops
	NormNoise // the same at an RMS of 0.5 whatever the cutoff, for low rumbles
)

// Voice is one sound layer.
type Voice struct {
	Wave      Wave
	F0, F1    float64 // start and end frequency (exponential sweep); F1 0: no sweep
	Amp       float64
	L, R      float64 // actuator gains
	Delay     float64 // s before it starts
	Attack    float64 // s
	Hold      float64 // s at full level
	Release   float64 // s
	TremHz    float64 // amplitude wobble
	TremDepth float64 // 0-1
	GateHz    float64 // on/off bursts per second, 0: continuous
	GateDuty  float64 // the part of each burst that is on
}

func (v Voice) length() float64 { return v.Delay + v.Attack + v.Hold + v.Release }

// oscillator keeps a voice's running state.
type oscillator struct {
	t     float64 // s since the start, delay included
	phase float64
	lp    float64 // noise low-pass state
}

// shot is a one-shot voice playing.
type shot struct {
	v    Voice
	osc  oscillator
	gain float64
}

// layer is a continuous voice whose level follows a target.
type layer struct {
	v      Voice
	target float64
	level  float64
	osc    oscillator
}

// maxShots: the oldest are dropped if something floods the synth.
const maxShots = 64

type Synth struct {
	mu     sync.Mutex
	shots  []*shot
	layers map[string]*layer
	master float64
	rng    uint32
}

func NewSynth() *Synth {
	return &Synth{layers: map[string]*layer{}, master: 1, rng: 0x12345678}
}

func (s *Synth) SetMaster(gain float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.master = gain
}

// Play starts a one-shot effect.
func (s *Synth) Play(voices []Voice, gain float64) {
	if gain <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range voices {
		s.shots = append(s.shots, &shot{v: v, gain: gain})
	}
	if len(s.shots) > maxShots {
		s.shots = s.shots[len(s.shots)-maxShots:]
	}
}

// SetLayer sets the level (0-1) of a continuous effect. Another voice for
// the same key (the weapon changed, say) replaces it.
func (s *Synth) SetLayer(key string, v Voice, level float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.layers[key]
	if !ok {
		if level <= 0 {
			return
		}
		l = &layer{}
		s.layers[key] = l
	}
	l.v, l.target = v, level
}

// StopAll fades everything out.
func (s *Synth) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shots = nil
	for _, l := range s.layers {
		l.target = 0
	}
}

// Render fills interleaved 4-channel frames: the speakers stay silent,
// channel 3 is the left actuator, channel 4 the right.
func (s *Synth) Render(out []int16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	const dt = 1.0 / dualsense.SampleRate
	smooth := 1 - math.Exp(-dt/0.012) // layers change level over ~12 ms
	for i := range len(out) / 4 {
		var l, r float64
		for _, sh := range s.shots {
			if sh.osc.t >= sh.v.length() {
				continue
			}
			x := s.sample(&sh.v, &sh.osc) * envelope(&sh.v, sh.osc.t) * sh.gain
			l += x * sh.v.L
			r += x * sh.v.R
			sh.osc.t += dt
		}
		for _, ly := range s.layers {
			ly.level += (ly.target - ly.level) * smooth
			if ly.level < 1e-4 && ly.target == 0 {
				continue
			}
			x := s.sample(&ly.v, &ly.osc) * ly.level
			l += x * ly.v.L
			r += x * ly.v.R
			ly.osc.t += dt
		}
		l, r = softClip(l*s.master), softClip(r*s.master)
		out[i*4+0], out[i*4+1] = 0, 0
		out[i*4+2] = int16(l * 32000)
		out[i*4+3] = int16(r * 32000)
	}
	playing := s.shots[:0]
	for _, sh := range s.shots {
		if sh.osc.t < sh.v.length() {
			playing = append(playing, sh)
		}
	}
	s.shots = playing
	for k, ly := range s.layers {
		if ly.target == 0 && ly.level < 1e-4 {
			delete(s.layers, k)
		}
	}
}

// sample renders the next sample of voice v.
func (s *Synth) sample(v *Voice, o *oscillator) float64 {
	dur := v.Attack + v.Hold + v.Release
	t := o.t - v.Delay
	if t < 0 {
		return 0
	}
	f := v.F0
	if v.F1 > 0 && dur > 0 {
		f = v.F0 * math.Pow(v.F1/v.F0, math.Min(1, t/dur))
	}
	var x float64
	if v.Wave == Noise || v.Wave == NormNoise {
		a := 1 - math.Exp(-2*math.Pi*f/dualsense.SampleRate)
		o.lp += a * (s.noise() - o.lp)
		if v.Wave == Noise {
			x = o.lp * 2.5
		} else {
			// a one-pole low-pass of uniform noise has std sqrt(a/(2-a)/3)
			x = math.Max(-1.6, math.Min(1.6, o.lp*0.5/math.Sqrt(a/(2-a)/3)))
		}
	} else {
		o.phase += f / dualsense.SampleRate
		x = waveform(v.Wave, o.phase)
	}
	if v.TremHz > 0 {
		x *= 1 - v.TremDepth*(0.5-0.5*math.Cos(2*math.Pi*v.TremHz*t))
	}
	if v.GateHz > 0 {
		if g := t * v.GateHz; g-math.Floor(g) > v.GateDuty {
			x = 0
		}
	}
	return x * v.Amp
}

// noise: xorshift white noise, -1..1.
func (s *Synth) noise() float64 {
	s.rng ^= s.rng << 13
	s.rng ^= s.rng >> 17
	s.rng ^= s.rng << 5
	return float64(s.rng)/float64(math.MaxUint32)*2 - 1
}

func waveform(w Wave, phase float64) float64 {
	x := phase - math.Floor(phase)
	switch w {
	case Square:
		if x < 0.5 {
			return 1
		}
		return -1
	case Triangle:
		return 1 - 4*math.Abs(x-0.5)
	case Saw:
		return 2*x - 1
	default:
		return math.Sin(2 * math.Pi * x)
	}
}

func envelope(v *Voice, t float64) float64 {
	t -= v.Delay
	switch {
	case t < 0:
		return 0
	case t < v.Attack:
		return t / v.Attack
	case t < v.Attack+v.Hold:
		return 1
	case v.Release > 0 && t < v.Attack+v.Hold+v.Release:
		return 1 - (t-v.Attack-v.Hold)/v.Release
	}
	return 0
}

func softClip(x float64) float64 {
	if x > 1 || x < -1 {
		return math.Tanh(x)
	}
	return x
}
