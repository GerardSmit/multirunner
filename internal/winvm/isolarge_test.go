package winvm

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kdomanski/iso9660"
)

func TestBuildISOFilesLargeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "runner.zip")
	payload := make([]byte, 100<<20) // 100 MB
	if _, err := rand.Read(payload[:1<<20]); err != nil {
		t.Fatal(err)
	}
	for i := 1 << 20; i < len(payload); i++ {
		payload[i] = byte(i)
	}
	if err := os.WriteFile(big, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(dir, "out.iso")
	if err := BuildISOFiles(iso, "AUTOUNATTEND", map[string]string{"a.txt": "hi"}, map[string]string{"runner.zip": big}); err != nil {
		t.Fatalf("BuildISOFiles: %v", err)
	}
	f, err := os.Open(iso)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := iso9660.OpenImage(f)
	if err != nil {
		t.Fatal(err)
	}
	root, err := img.RootDir()
	if err != nil {
		t.Fatal(err)
	}
	children, err := root.GetChildren()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range children {
		if c.Name() == "runner.zip" || c.Name() == "RUNNER.ZIP" {
			found = true
			if c.Size() != int64(len(payload)) {
				t.Fatalf("size mismatch: iso=%d want=%d", c.Size(), len(payload))
			}
			rc := c.Reader()
			got, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("content mismatch: got %d bytes", len(got))
			}
		}
	}
	if !found {
		t.Fatalf("runner.zip not found on ISO; children=%v", names(children))
	}
}

func names(cs []*iso9660.File) []string {
	var n []string
	for _, c := range cs {
		n = append(n, c.Name())
	}
	return n
}
