// Package elite reads what Elite Dangerous writes for third-party tools:
// Status.json and the journal (Frontier's Journal Manual).
package elite

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// Flags are Status.json's Flags bits.
type Flags uint64

const (
	Docked             Flags = 1 << 0
	Landed             Flags = 1 << 1
	ShieldsUp          Flags = 1 << 3
	Supercruise        Flags = 1 << 4
	HardpointsDeployed Flags = 1 << 6
	SilentRunning      Flags = 1 << 10
	ScoopingFuel       Flags = 1 << 11
	SRVTurretView      Flags = 1 << 13
	FSDCharging        Flags = 1 << 17
	LowFuel            Flags = 1 << 19
	Overheating        Flags = 1 << 20
	InDanger           Flags = 1 << 22
	BeingInterdicted   Flags = 1 << 23
	InMainShip         Flags = 1 << 24
	InFighter          Flags = 1 << 25
	InSRV              Flags = 1 << 26
	AnalysisMode       Flags = 1 << 27
	FSDJump            Flags = 1 << 30
)

// Has reports whether any of the given bits is set.
func (f Flags) Has(bits Flags) bool { return f&bits != 0 }

// Flags2 are Status.json's Flags2 bits (Odyssey).
type Flags2 uint64

const (
	OnFoot            Flags2 = 1 << 0
	OnFootInStation   Flags2 = 1 << 3
	OnFootOnPlanet    Flags2 = 1 << 4
	LowOxygen         Flags2 = 1 << 6
	OnFootInHangar    Flags2 = 1 << 13
	OnFootSocialSpace Flags2 = 1 << 14
)

func (f Flags2) Has(bits Flags2) bool { return f&bits != 0 }

// NoPanel is GuiFocus with no panel or map open.
const NoPanel = 0

type Status struct {
	Flags     Flags    `json:"Flags"`
	Flags2    Flags2   `json:"Flags2"`
	FireGroup int      `json:"FireGroup"` // 0-based
	GuiFocus  int      `json:"GuiFocus"`
	Health    *float64 `json:"Health"` // on foot, 0-1
}

func (s Status) InShip() bool  { return s.Flags.Has(InMainShip | InFighter) }
func (s Status) Parked() bool  { return s.Flags.Has(Docked | Landed) }
func (s Status) OnFoot() bool  { return s.Flags2.Has(OnFoot) }
func (s Status) InPanel() bool { return s.GuiFocus != NoPanel }

// StatusReader re-reads Status.json when it changes. Elite rewrites the file
// in place, so a read can catch it empty or half written: the last good one
// is kept.
type StatusReader struct {
	path    string
	lastRaw []byte
	status  Status
}

func NewStatusReader(journalDir string) *StatusReader {
	return &StatusReader{path: filepath.Join(journalDir, "Status.json")}
}

// Poll returns the status and whether it changed since the last call.
func (r *StatusReader) Poll() (Status, bool) {
	raw, err := os.ReadFile(r.path)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(raw, r.lastRaw) {
		return r.status, false
	}
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return r.status, false
	}
	r.lastRaw, r.status = raw, s
	return s, true
}
