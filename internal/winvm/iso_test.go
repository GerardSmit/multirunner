package winvm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildJITISO(t *testing.T) {
	p := filepath.Join(t.TempDir(), "jit.iso")
	if err := BuildJITISO(p, "BASE64-JIT-BLOB", map[string]string{"ACTIONS_RESULTS_URL": "http://10.0.2.2:3000/"}); err != nil {
		t.Fatalf("BuildJITISO: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() == 0 {
		t.Error("iso is empty")
	}
}
