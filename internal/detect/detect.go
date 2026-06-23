// Package detect scans a repository (a local checkout or, via the GitHub API, a
// remote repo) and recommends which prebuilt runner image flavors its CI is
// likely to need. It is a setup-time helper: routing at run time stays label
// based, so the output is a ready-to-paste pools block plus the images to pull.
package detect

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Flavor is a published runner image flavor (matches config.publishedFlavors).
type Flavor string

const (
	FlavorNativeBuild Flavor = "native-build"
	FlavorNode        Flavor = "node"
	FlavorDotnet      Flavor = "dotnet"
	FlavorRust        Flavor = "rust"
	FlavorGo          Flavor = "go"
)

// Source provides the file paths and (for workflows) file contents of a repo.
// Paths are repo-relative and slash-separated.
type Source interface {
	Paths() ([]string, error)
	ReadFile(path string) ([]byte, error)
}

// Hit records a detected flavor and a few example files that triggered it.
type Hit struct {
	Flavor   Flavor
	Evidence []string
}

// Result is the outcome of a scan.
type Result struct {
	Hits   []Hit      // detected flavors, in catalog order
	RunsOn [][]string // distinct runs-on label sets found in workflows
}

// marker maps a filename/extension signal to the flavor it implies.
type marker struct {
	flavor Flavor
	// match reports whether a repo-relative path is evidence for this flavor.
	match func(base, ext string) bool
}

var markers = []marker{
	{FlavorDotnet, func(base, ext string) bool { return ext == ".csproj" || ext == ".sln" || base == "global.json" }},
	{FlavorNode, func(base, ext string) bool { return base == "package.json" }},
	{FlavorGo, func(base, ext string) bool { return base == "go.mod" }},
	{FlavorRust, func(base, ext string) bool { return base == "Cargo.toml" }},
	{FlavorNativeBuild, func(base, ext string) bool {
		return base == "pyproject.toml" || base == "requirements.txt" || base == "setup.py"
	}},
}

// catalogOrder is the stable display order for detected flavors.
var catalogOrder = []Flavor{FlavorNativeBuild, FlavorNode, FlavorDotnet, FlavorRust, FlavorGo}

// skipDir reports directories whose contents are dependency/build output and
// should not contribute markers (they'd produce false positives like a vendored
// package.json deep under node_modules).
func skipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", ".git", "bin", "obj", "target", "dist", "build", ".venv", "venv":
		return true
	}
	return false
}

// Scan inspects the source and returns detected flavors + workflow runs-on sets.
func Scan(src Source) (*Result, error) {
	paths, err := src.Paths()
	if err != nil {
		return nil, err
	}

	evidence := map[Flavor][]string{}
	var workflows []string
	for _, p := range paths {
		p = strings.TrimPrefix(path.Clean(strings.ReplaceAll(p, "\\", "/")), "./")
		if inSkippedDir(p) {
			continue
		}
		base := path.Base(p)
		ext := strings.ToLower(path.Ext(p))
		for _, m := range markers {
			if m.match(base, ext) {
				if len(evidence[m.flavor]) < 5 {
					evidence[m.flavor] = append(evidence[m.flavor], p)
				}
			}
		}
		if isWorkflow(p) {
			workflows = append(workflows, p)
		}
	}

	res := &Result{}
	for _, f := range catalogOrder {
		if ev := evidence[f]; len(ev) > 0 {
			res.Hits = append(res.Hits, Hit{Flavor: f, Evidence: ev})
		}
	}
	res.RunsOn = collectRunsOn(src, workflows)
	return res, nil
}

func inSkippedDir(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if skipDir(seg) {
			return true
		}
	}
	return false
}

func isWorkflow(p string) bool {
	if !strings.HasPrefix(p, ".github/workflows/") {
		return false
	}
	ext := strings.ToLower(path.Ext(p))
	return ext == ".yml" || ext == ".yaml"
}

// collectRunsOn parses each workflow's job runs-on values into distinct label
// sets. A parse error on one workflow is skipped (best-effort reporting).
func collectRunsOn(src Source, workflows []string) [][]string {
	seen := map[string]bool{}
	var out [][]string
	for _, wf := range workflows {
		data, err := src.ReadFile(wf)
		if err != nil {
			continue
		}
		for _, labels := range runsOnFromWorkflow(data) {
			key := strings.Join(labels, ",")
			if !seen[key] {
				seen[key] = true
				out = append(out, labels)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.Join(out[i], ",") < strings.Join(out[j], ",") })
	return out
}

// runsOnFromWorkflow extracts every job's runs-on as a string slice. runs-on may
// be a scalar, a sequence, or (rarely) a matrix expression; expressions are kept
// verbatim so the user can see them.
func runsOnFromWorkflow(data []byte) [][]string {
	var wf struct {
		Jobs map[string]struct {
			RunsOn yaml.Node `yaml:"runs-on"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil
	}
	var out [][]string
	for _, job := range wf.Jobs {
		labels := nodeToStrings(job.RunsOn)
		if len(labels) > 0 {
			out = append(out, labels)
		}
	}
	return out
}

func nodeToStrings(n yaml.Node) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" {
			return nil
		}
		return []string{n.Value}
	case yaml.SequenceNode:
		var s []string
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && c.Value != "" {
				s = append(s, c.Value)
			}
		}
		return s
	default:
		return nil
	}
}

// Render produces a human report: detected flavors, the images they map to, a
// ready-to-paste pools block, and the runs-on label sets found in workflows.
func Render(res *Result, os string) string {
	var b strings.Builder
	if len(res.Hits) == 0 {
		b.WriteString("No language markers detected. The minimal image is enough:\n")
		b.WriteString("  image_tier: minimal\n")
	} else {
		b.WriteString("Detected flavors:\n")
		for _, h := range res.Hits {
			fmt.Fprintf(&b, "  - %-12s (%s)\n", h.Flavor, strings.Join(h.Evidence, ", "))
		}
		if hasFlavor(res.Hits, FlavorDotnet) || hasFlavor(res.Hits, FlavorRust) {
			b.WriteString("\nNote: the dotnet and rust flavors already bundle Node.\n")
		}

		b.WriteString("\nRecommended pools (paste into config.yaml):\n\n")
		b.WriteString("pools:\n")
		for _, h := range res.Hits {
			fmt.Fprintf(&b, "  - name: %s-%s\n", os, h.Flavor)
			fmt.Fprintf(&b, "    os: %s\n", os)
			fmt.Fprintf(&b, "    image_tier: %s\n", h.Flavor)
			fmt.Fprintf(&b, "    labels: [self-hosted, %s, %s]\n", os, h.Flavor)
		}

		b.WriteString("\nImages to pull:\n")
		for _, h := range res.Hits {
			fmt.Fprintf(&b, "  gerardsmit/multirunner-runner-%s:%s\n", os, h.Flavor)
		}
	}

	if len(res.RunsOn) > 0 {
		b.WriteString("\nruns-on values already used in .github/workflows:\n")
		for _, labels := range res.RunsOn {
			fmt.Fprintf(&b, "  [%s]\n", strings.Join(labels, ", "))
		}
		b.WriteString("\nAdd the flavor label to a job's runs-on to route it, e.g.\n")
		if len(res.Hits) > 0 {
			fmt.Fprintf(&b, "  runs-on: [self-hosted, %s, %s]\n", os, res.Hits[0].Flavor)
		}
	}
	return b.String()
}

func hasFlavor(hits []Hit, f Flavor) bool {
	for _, h := range hits {
		if h.Flavor == f {
			return true
		}
	}
	return false
}
