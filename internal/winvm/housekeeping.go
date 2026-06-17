// Package winvm provides the Windows-VM (QEMU) runner backend: a golden Server
// Core image is baked once, ephemeral jobs run on throwaway copy-on-write
// overlays, and housekeeping keeps the golden image's eval license valid and its
// tools fresh.
package winvm

import "time"

// Action is what housekeeping decides to do with the golden image.
type Action int

const (
	ActionNone    Action = iota // golden is fine
	ActionRearm                 // reset the eval clock (slmgr /rearm) + re-snapshot
	ActionRebuild               // full reinstall + re-bake (rearms exhausted or tools stale)
)

func (a Action) String() string {
	switch a {
	case ActionRearm:
		return "rearm"
	case ActionRebuild:
		return "rebuild"
	default:
		return "none"
	}
}

// GoldenMeta is the sidecar metadata stored next to the golden image.
type GoldenMeta struct {
	// CreatedAt is when the eval clock last reset (initial build or last rearm).
	CreatedAt time.Time `json:"created_at"`
	// EvalDays is the eval window length (180 for Server, 90 for client).
	EvalDays int `json:"eval_days"`
	// RearmsUsed / MaxRearms track remaining slmgr rearms.
	RearmsUsed int `json:"rearms_used"`
	MaxRearms  int `json:"max_rearms"`
	// Licensed is true when a real key/KMS is configured (eval housekeeping off).
	Licensed bool `json:"licensed"`
	// WorkflowsHash fingerprints the baked toolset so changes trigger a rebuild.
	WorkflowsHash string `json:"workflows_hash"`
}

// Policy parameterizes housekeeping decisions.
type Policy struct {
	// ThresholdDays: act when fewer than this many eval days remain.
	ThresholdDays int
}

// DefaultPolicy returns sensible defaults (act 14 days before expiry).
func DefaultPolicy() Policy { return Policy{ThresholdDays: 14} }

// DaysLeft returns the eval days remaining for the golden image at time now.
func (m GoldenMeta) DaysLeft(now time.Time) int {
	if m.Licensed {
		return 1 << 30 // effectively infinite
	}
	elapsed := int(now.Sub(m.CreatedAt).Hours() / 24)
	return m.EvalDays - elapsed
}

// Decide chooses the housekeeping action for the golden image. wantHash is the
// current desired workflows/tools fingerprint; a mismatch forces a rebuild.
func (p Policy) Decide(m GoldenMeta, wantHash string, now time.Time) Action {
	// Tool/workflow drift always wins — a rearm wouldn't update the toolset.
	if wantHash != "" && wantHash != m.WorkflowsHash {
		return ActionRebuild
	}
	if m.Licensed {
		return ActionNone
	}
	if m.DaysLeft(now) >= p.ThresholdDays {
		return ActionNone
	}
	if m.RearmsUsed < m.MaxRearms {
		return ActionRearm
	}
	return ActionRebuild
}

// ApplyRearm records a rearm: reset the clock and consume one rearm.
func (m GoldenMeta) ApplyRearm(now time.Time) GoldenMeta {
	m.CreatedAt = now
	m.RearmsUsed++
	return m
}

// ApplyRebuild records a fresh build: reset clock, rearms, and tool fingerprint.
func (m GoldenMeta) ApplyRebuild(now time.Time, wantHash string) GoldenMeta {
	m.CreatedAt = now
	m.RearmsUsed = 0
	m.WorkflowsHash = wantHash
	return m
}
