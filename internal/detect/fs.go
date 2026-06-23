package detect

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DirSource scans a local checkout rooted at Root.
type DirSource struct {
	Root string
}

// Paths walks the directory tree, skipping dependency/output dirs, and returns
// repo-relative slash-separated file paths.
func (d DirSource) Paths() ([]string, error) {
	var out []string
	err := filepath.WalkDir(d.Root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if p != d.Root && skipDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(d.Root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReadFile reads a repo-relative path from the checkout.
func (d DirSource) ReadFile(p string) ([]byte, error) {
	return os.ReadFile(filepath.Join(d.Root, filepath.FromSlash(strings.TrimPrefix(p, "/"))))
}
