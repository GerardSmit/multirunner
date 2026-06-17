package cache

import (
	"bytes"
	"context"
	"testing"
)

// putEntry creates a finalized cache entry with one part of the given size and
// returns its folder id.
func putEntry(t *testing.T, s *store, key string, size int) string {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.createUpload(ctx, key, "v1", "default", "repo1"); err != nil {
		t.Fatalf("createUpload: %v", err)
	}
	var up struct{ id int64 }
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM uploads WHERE key=? AND version=? AND scope=? AND repo_id=?`,
		key, "v1", "default", "repo1").Scan(&up.id); err != nil {
		t.Fatalf("lookup upload: %v", err)
	}
	if err := s.uploadPart(ctx, up.id, 0, bytes.NewReader(make([]byte, size))); err != nil {
		t.Fatalf("uploadPart: %v", err)
	}
	id, err := s.completeUpload(ctx, key, "v1", "default", "repo1")
	if err != nil {
		t.Fatalf("completeUpload: %v", err)
	}
	return id
}

func setLastUsed(t *testing.T, s *store, id string, millis int64) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE cache_entries SET last_used_at=? WHERE id=?`, millis, id); err != nil {
		t.Fatalf("setLastUsed: %v", err)
	}
}

func countEntries(t *testing.T, s *store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cache_entries`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestEvictByAge(t *testing.T) {
	ctx := context.Background()
	s, err := openStore(ctx, t.TempDir()+"/c.db", t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer s.Close()

	old := putEntry(t, s, "old", 100)
	fresh := putEntry(t, s, "fresh", 100)
	setLastUsed(t, s, old, 1000)          // long ago
	setLastUsed(t, s, fresh, nowMillis()) // just now
	cutoff := nowMillis() - 60_000        // anything older than 1 min ago

	n, err := s.evict(ctx, cutoff, 0)
	if err != nil {
		t.Fatalf("evict: %v", err)
	}
	if n != 1 {
		t.Fatalf("evicted %d, want 1", n)
	}
	if got := countEntries(t, s); got != 1 {
		t.Fatalf("remaining %d, want 1", got)
	}
	if _, ok, _ := s.getEntry(ctx, fresh); !ok {
		t.Fatalf("fresh entry was evicted")
	}
	if _, ok, _ := s.getEntry(ctx, old); ok {
		t.Fatalf("old entry survived")
	}
}

func TestEvictBySize(t *testing.T) {
	ctx := context.Background()
	s, err := openStore(ctx, t.TempDir()+"/c.db", t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer s.Close()

	a := putEntry(t, s, "a", 1000)
	b := putEntry(t, s, "b", 1000)
	c := putEntry(t, s, "c", 1000)
	setLastUsed(t, s, a, 1000) // least recently used
	setLastUsed(t, s, b, 2000)
	setLastUsed(t, s, c, 3000) // most recently used

	// Cap at ~1500 bytes => must evict the two LRU entries, keep c.
	n, err := s.evict(ctx, 0, 1500)
	if err != nil {
		t.Fatalf("evict: %v", err)
	}
	if n != 2 {
		t.Fatalf("evicted %d, want 2", n)
	}
	if _, ok, _ := s.getEntry(ctx, c); !ok {
		t.Fatalf("most-recent entry c was evicted")
	}
	if _, ok, _ := s.getEntry(ctx, a); ok {
		t.Fatalf("LRU entry a survived")
	}
}

func TestEvictDisabledKeepsAll(t *testing.T) {
	ctx := context.Background()
	s, err := openStore(ctx, t.TempDir()+"/c.db", t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer s.Close()

	putEntry(t, s, "a", 100)
	setLastUsed(t, s, putEntry(t, s, "b", 100), 1000)

	n, err := s.evict(ctx, 0, 0) // both knobs off
	if err != nil {
		t.Fatalf("evict: %v", err)
	}
	if n != 0 || countEntries(t, s) != 2 {
		t.Fatalf("evict removed entries while disabled: n=%d remaining=%d", n, countEntries(t, s))
	}
}
