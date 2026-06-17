package winvm

import (
	"strings"
	"testing"
)

func TestDetectAccel(t *testing.T) {
	cases := map[string]string{"linux": "kvm", "windows": "whpx", "darwin": "hvf", "plan9": "tcg"}
	for goos, want := range cases {
		if got := DetectAccel(goos); got != want {
			t.Errorf("DetectAccel(%s) = %s, want %s", goos, got, want)
		}
	}
}

func TestOverlayCreateArgs(t *testing.T) {
	got := strings.Join(OverlayCreateArgs("golden.qcow2", "job.qcow2"), " ")
	for _, want := range []string{"create", "-b golden.qcow2", "job.qcow2", "-f qcow2"} {
		if !strings.Contains(got, want) {
			t.Errorf("overlay args missing %q: %s", want, got)
		}
	}
}

func TestQEMUArgs(t *testing.T) {
	got := strings.Join(QEMUArgs(LaunchOpts{
		Overlay: "job.qcow2", JITISOPath: "jit.iso", MemMB: 8192, CPUs: 4, Accel: "kvm",
	}), " ")
	for _, want := range []string{
		"-accel kvm", "-m 8192", "-smp 4",
		"file=job.qcow2,if=none,id=osdisk,format=qcow2", "ide-hd,drive=osdisk",
		"file=jit.iso,media=cdrom,readonly=on",
		"-display none", "e1000", "-serial",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("qemu args missing %q:\n%s", want, got)
		}
	}
}

func TestQEMUArgsDefaults(t *testing.T) {
	got := strings.Join(QEMUArgs(LaunchOpts{Overlay: "j.qcow2"}), " ")
	if !strings.Contains(got, "-m 4096") || !strings.Contains(got, "-smp 2") || !strings.Contains(got, "-accel tcg") {
		t.Errorf("defaults not applied: %s", got)
	}
	if strings.Contains(got, "media=cdrom") {
		t.Error("no cdrom expected when JITISOPath empty")
	}
}
