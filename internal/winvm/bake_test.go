package winvm

import (
	"strings"
	"testing"
	"time"
)

func TestAutounattendFiles(t *testing.T) {
	files, err := AutounattendFiles("9.9.9", "P@ss123", []string{"dotnet", "node"})
	if err != nil {
		t.Fatal(err)
	}
	install := files["install-golden.ps1"]
	if !strings.Contains(install, "9.9.9") {
		t.Error("runner version not substituted")
	}
	if strings.Contains(install, "__RUNNER_VERSION__") {
		t.Error("runner version placeholder left")
	}
	for _, ph := range []string{"__TOOLS__", "__NODE_URL__", "__GO_URL__"} {
		if strings.Contains(install, ph) {
			t.Errorf("placeholder %s left in script", ph)
		}
	}
	if !strings.Contains(install, "dotnet,node") {
		t.Error("tools list not substituted")
	}
	if !strings.Contains(files["autounattend.xml"], "P@ss123") {
		t.Error("admin password not substituted")
	}
	if _, ok := files["startup.ps1"]; !ok {
		t.Error("startup.ps1 missing")
	}
}

func TestToolsHash(t *testing.T) {
	if ToolsHash(nil) != "" {
		t.Error("empty tools should hash to empty")
	}
	// Order- and case-insensitive, de-duped.
	if ToolsHash([]string{"node", "dotnet"}) != ToolsHash([]string{"DotNet", "node", "node"}) {
		t.Error("ToolsHash should be normalized")
	}
	if ToolsHash([]string{"node"}) == ToolsHash([]string{"go"}) {
		t.Error("different tools should hash differently")
	}
}

func TestBakeQEMUArgs(t *testing.T) {
	got := strings.Join(bakeQEMUArgs(BakeOptions{
		Golden: "g.qcow2", WindowsISO: "win.iso", MemMB: 4096, CPUs: 2, Accel: "kvm",
		OVMFCode: "code.fd",
	}, "auto.iso", "vars.fd"), " ")
	for _, want := range []string{
		"-accel kvm", "file=g.qcow2,if=none,id=osdisk", "file=win.iso,media=cdrom",
		"file=auto.iso,media=cdrom", "e1000", "-qmp", "-serial",
		"if=pflash,format=raw,unit=0,readonly=on,file=code.fd", "unit=1,file=vars.fd",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bake args missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-no-reboot") {
		t.Error("bake must NOT use -no-reboot (Windows Setup reboots mid-install)")
	}
}

func TestMetaRoundTrip(t *testing.T) {
	golden := t.TempDir() + "/golden.qcow2"
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	in := GoldenMeta{CreatedAt: now, EvalDays: 180, MaxRearms: 5, RearmsUsed: 1, WorkflowsHash: "abc"}
	if err := SaveMeta(golden, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadMeta(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !out.CreatedAt.Equal(now) || out.EvalDays != 180 || out.RearmsUsed != 1 || out.WorkflowsHash != "abc" {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

func TestLoadMetaMissing(t *testing.T) {
	m, err := LoadMeta(t.TempDir() + "/nope.qcow2")
	if err != nil {
		t.Fatalf("missing meta should not error: %v", err)
	}
	if m.EvalDays != 0 {
		t.Errorf("expected zero meta, got %+v", m)
	}
}
