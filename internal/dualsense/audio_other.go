//go:build !windows

package dualsense

// Native haptics are only streamed on Windows.

func ListAudio() []AudioDevice { return nil }

type HapticsOut struct{}

func NewHapticsOut(render func(frames []int16)) *HapticsOut { return &HapticsOut{} }
func NewHapticsOutFor(render func(frames []int16), opts AudioOptions) *HapticsOut {
	return &HapticsOut{}
}
func (a *HapticsOut) Active() bool { return false }
func (a *HapticsOut) Maintain()    {}
func (a *HapticsOut) Close()       {}
