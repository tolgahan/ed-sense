package gyro

import (
	"encoding/json"
	"math"
	"os"
)

// BiasFile keeps the gyro's drift between runs, in the data folder next to
// edsense.json.
const BiasFile = "gyro_calibration.json"

type biasJSON struct {
	Bias []float64 `json:"bias"` // deg/s, X Y Z
}

// LoadBias reads a saved drift. False: no file, or not a usable one.
func LoadBias(path string) ([3]float64, bool) {
	var b [3]float64
	raw, err := os.ReadFile(path)
	if err != nil {
		return b, false
	}
	var f biasJSON
	if json.Unmarshal(raw, &f) != nil || len(f.Bias) != len(b) {
		return b, false
	}
	copy(b[:], f.Bias)
	return b, true
}

// SaveBias writes the drift as {"bias":[x,y,z]}, rounded to 0.0001 deg/s,
// far under the gyro's noise, so the file stays readable.
func SaveBias(path string, b [3]float64) error {
	f := biasJSON{Bias: make([]float64, len(b))}
	for i, v := range b {
		f.Bias[i] = math.Round(v*1e4) / 1e4
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
