package gitcache

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepRemovesStaleMirrors(t *testing.T) {
	base := setupSourceRepo(t)
	mirrorRoot := t.TempDir()
	m, err := New(mirrorRoot, base, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	path, err := m.EnsureMirror(ctx, "repo")
	if err != nil {
		t.Fatalf("EnsureMirror: %v", err)
	}

	// Backdate the last-used marker to 40 days ago.
	marker := filepath.Join(path, lastUsedFile)
	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	n, err := m.Sweep(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}
	if mirrorExists(path) {
		t.Fatalf("stale mirror not removed")
	}
}

func TestSweepKeepsFreshMirrors(t *testing.T) {
	base := setupSourceRepo(t)
	mirrorRoot := t.TempDir()
	m, err := New(mirrorRoot, base, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	path, err := m.EnsureMirror(ctx, "repo")
	if err != nil {
		t.Fatalf("EnsureMirror: %v", err)
	}

	n, err := m.Sweep(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d, want 0", n)
	}
	if !mirrorExists(path) {
		t.Fatalf("fresh mirror was removed")
	}
}

func TestSweepDisabled(t *testing.T) {
	base := setupSourceRepo(t)
	mirrorRoot := t.TempDir()
	m, err := New(mirrorRoot, base, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.EnsureMirror(ctx, "repo"); err != nil {
		t.Fatalf("EnsureMirror: %v", err)
	}
	if n, err := m.Sweep(ctx, 0); err != nil || n != 0 {
		t.Fatalf("Sweep disabled: n=%d err=%v", n, err)
	}
}
