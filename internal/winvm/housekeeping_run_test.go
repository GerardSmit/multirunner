package winvm

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestHousekeeperDispatch(t *testing.T) {
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	// Three goldens: healthy, near-expiry (rearm), tool-drift (rebuild).
	healthy := filepath.Join(dir, "healthy.qcow2")
	rearm := filepath.Join(dir, "rearm.qcow2")
	rebuild := filepath.Join(dir, "rebuild.qcow2")

	must := func(g string, m GoldenMeta) {
		if err := SaveMeta(g, m); err != nil {
			t.Fatal(err)
		}
	}
	must(healthy, GoldenMeta{CreatedAt: now.AddDate(0, 0, -10), EvalDays: 180, MaxRearms: 5, WorkflowsHash: "h"})
	must(rearm, GoldenMeta{CreatedAt: now.AddDate(0, 0, -175), EvalDays: 180, MaxRearms: 5, WorkflowsHash: "h"})
	must(rebuild, GoldenMeta{CreatedAt: now.AddDate(0, 0, -1), EvalDays: 180, MaxRearms: 5, WorkflowsHash: "old"})

	refs := []GoldenRef{
		{Bake: BakeOptions{Golden: healthy, WorkflowsHash: "h"}},
		{Bake: BakeOptions{Golden: rearm, WorkflowsHash: "h"}},
		{Bake: BakeOptions{Golden: rebuild, WorkflowsHash: "new", WindowsISO: "win.iso"}},
	}

	var rearmed, rebuilt []string
	hk := NewHousekeeper(refs, DefaultPolicy(), 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hk.now = func() time.Time { return now }
	hk.acts = ActionFuncs{
		Rearm: func(_ context.Context, r GoldenRef, _ time.Time) error {
			rearmed = append(rearmed, r.Bake.Golden)
			return nil
		},
		Rebuild: func(_ context.Context, r GoldenRef, _ time.Time) error {
			rebuilt = append(rebuilt, r.Bake.Golden)
			return nil
		},
	}

	hk.CheckOnce(context.Background())

	if len(rearmed) != 1 || rearmed[0] != rearm {
		t.Errorf("rearmed = %v, want [%s]", rearmed, rearm)
	}
	if len(rebuilt) != 1 || rebuilt[0] != rebuild {
		t.Errorf("rebuilt = %v, want [%s]", rebuilt, rebuild)
	}
}

func TestHousekeeperSkipsRebuildWithoutISO(t *testing.T) {
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	g := filepath.Join(t.TempDir(), "drift.qcow2")
	_ = SaveMeta(g, GoldenMeta{CreatedAt: now, EvalDays: 180, MaxRearms: 5, WorkflowsHash: "old"})

	called := false
	hk := NewHousekeeper(
		[]GoldenRef{{Bake: BakeOptions{Golden: g, WorkflowsHash: "new"}}}, // no WindowsISO
		DefaultPolicy(), 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hk.now = func() time.Time { return now }
	hk.acts = ActionFuncs{Rebuild: func(context.Context, GoldenRef, time.Time) error { called = true; return nil }}
	hk.CheckOnce(context.Background())
	if called {
		t.Error("rebuild should be skipped when WindowsISO is empty")
	}
}
