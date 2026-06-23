package detect

import (
	"strings"
	"testing"
)

type memSource struct {
	files map[string]string
}

func (m memSource) Paths() ([]string, error) {
	out := make([]string, 0, len(m.files))
	for p := range m.files {
		out = append(out, p)
	}
	return out, nil
}

func (m memSource) ReadFile(p string) ([]byte, error) {
	return []byte(m.files[p]), nil
}

func TestScanFlavors(t *testing.T) {
	src := memSource{files: map[string]string{
		"src/App.csproj":                "<Project/>",
		"web/package.json":              "{}",
		"node_modules/dep/package.json": "{}", // skipped: under node_modules
		"go.mod":                        "module x",
		"crate/Cargo.toml":              "[package]",
		"pyproject.toml":                "[tool.poetry]",
	}}
	res, err := Scan(src)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := map[Flavor]bool{}
	for _, h := range res.Hits {
		got[h.Flavor] = true
	}
	for _, want := range []Flavor{FlavorDotnet, FlavorNode, FlavorGo, FlavorRust, FlavorNativeBuild} {
		if !got[want] {
			t.Errorf("missing flavor %s", want)
		}
	}
	// The vendored package.json must not be counted as extra evidence.
	for _, h := range res.Hits {
		if h.Flavor == FlavorNode {
			for _, e := range h.Evidence {
				if strings.Contains(e, "node_modules") {
					t.Errorf("node_modules evidence leaked: %v", h.Evidence)
				}
			}
		}
	}
}

func TestScanRunsOn(t *testing.T) {
	src := memSource{files: map[string]string{
		"go.mod": "module x",
		".github/workflows/ci.yml": `
name: ci
jobs:
  build:
    runs-on: [self-hosted, linux, dotnet]
    steps: []
  test:
    runs-on: ubuntu-latest
    steps: []
`,
	}}
	res, err := Scan(src)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.RunsOn) != 2 {
		t.Fatalf("runs-on sets = %v, want 2", res.RunsOn)
	}
	out := Render(res, "linux")
	if !strings.Contains(out, "self-hosted, linux, dotnet") {
		t.Errorf("render missing runs-on set:\n%s", out)
	}
	if !strings.Contains(out, "image_tier: go") {
		t.Errorf("render missing go pool:\n%s", out)
	}
}

func TestScanEmpty(t *testing.T) {
	res, err := Scan(memSource{files: map[string]string{"README.md": "# hi"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("expected no hits, got %v", res.Hits)
	}
	if !strings.Contains(Render(res, "linux"), "minimal") {
		t.Error("empty render should suggest minimal")
	}
}
