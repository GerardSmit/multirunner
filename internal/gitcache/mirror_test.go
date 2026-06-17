package gitcache

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupSourceRepo creates <baseDir>/repo.git with one commit and returns the
// base dir (as a clone base) using forward slashes.
func setupSourceRepo(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "repo.git")
	mustGit(t, base, "init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "a.txt")
	mustGit(t, repo, "commit", "-m", "first")
	return filepath.ToSlash(base)
}

func TestEnsureMirrorCloneThenFetch(t *testing.T) {
	base := setupSourceRepo(t)
	repoDir := filepath.FromSlash(base + "/repo.git")
	mirrorRoot := t.TempDir()

	m, err := New(mirrorRoot, base, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// First call clones the mirror.
	path, err := m.EnsureMirror(ctx, "repo")
	if err != nil {
		t.Fatalf("EnsureMirror clone: %v", err)
	}
	if !mirrorExists(path) {
		t.Fatalf("mirror not created at %s", path)
	}
	if n := commitCount(t, path); n != 1 {
		t.Fatalf("mirror commit count = %d, want 1", n)
	}

	// Add a second commit to the source.
	if err := os.WriteFile(filepath.Join(repoDir, "b.txt"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", "b.txt")
	mustGit(t, repoDir, "commit", "-m", "second")

	// Second call fetches the update.
	if _, err := m.EnsureMirror(ctx, "repo"); err != nil {
		t.Fatalf("EnsureMirror fetch: %v", err)
	}
	if n := commitCount(t, path); n != 2 {
		t.Fatalf("mirror commit count after fetch = %d, want 2", n)
	}
}

func TestEnsureMirrorConcurrent(t *testing.T) {
	base := setupSourceRepo(t)
	m, err := New(t.TempDir(), base, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = m.EnsureMirror(ctx, "repo")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

func TestContainerPath(t *testing.T) {
	m := &Manager{}
	if got := m.ContainerPath("octo/hello", "linux"); got != "/gitcache/octo/hello.git" {
		t.Errorf("linux ContainerPath = %q", got)
	}
	if got := m.ContainerPath("octo/hello", "windows"); got != `C:\gitcache\octo\hello.git` {
		t.Errorf("windows ContainerPath = %q", got)
	}
}

func commitCount(t *testing.T, mirrorPath string) int {
	t.Helper()
	out := mustGit(t, mirrorPath, "rev-list", "--count", "main")
	n := 0
	for _, c := range out {
		if c < '0' || c > '9' {
			continue
		}
		n = n*10 + int(c-'0')
	}
	return n
}
