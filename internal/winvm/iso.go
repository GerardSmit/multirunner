package winvm

import (
	"os"
	"sort"
	"strings"

	"github.com/kdomanski/iso9660"
)

// jitFileName is where the guest startup task reads the JIT config from (on the
// attached CD-ROM, e.g. D:\jitconfig.txt). envFileName carries the runner env
// (KEY=VAL lines: cache redirect, etc.) that startup.ps1 applies before run.cmd.
const (
	jitFileName = "jitconfig.txt"
	envFileName = "runnerenv.txt"
)

// BuildJITISO writes a tiny ISO carrying the JIT config blob and the runner env,
// attached to the VM as a CD-ROM. Pure Go (no mkisofs/oscdimg), so it works on
// Linux and Windows.
func BuildJITISO(path, jit string, env map[string]string) error {
	files := map[string]string{jitFileName: jit}
	if len(env) > 0 {
		files[envFileName] = renderEnv(env)
	}
	return BuildISO(path, "MRJIT", files)
}

// renderEnv formats env as sorted CRLF-terminated KEY=VAL lines for the guest.
func renderEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + env[k] + "\r\n")
	}
	return b.String()
}

// BuildISO writes an ISO containing the given files (name -> content). Used for
// the JIT config disk and the bake-time autounattend disk.
func BuildISO(path, label string, files map[string]string) error {
	return BuildISOFiles(path, label, files, nil)
}

// BuildISOFiles writes an ISO with inline string files (name -> content) plus
// optional on-disk files (name -> host path) streamed from disk instead of held
// in memory — used to stage large binaries (runner/MinGit) onto the bake ISO so
// the guest reads them off the virtual CD instead of over the (slow) VM network.
func BuildISOFiles(path, label string, files map[string]string, fileRefs map[string]string) error {
	w, err := iso9660.NewWriter()
	if err != nil {
		return err
	}
	defer w.Cleanup()

	for name, content := range files {
		if err := w.AddFile(strings.NewReader(content), name); err != nil {
			return err
		}
	}
	for name, hostPath := range fileRefs {
		src, err := os.Open(hostPath)
		if err != nil {
			return err
		}
		err = w.AddFile(src, name)
		src.Close()
		if err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return w.WriteTo(f, label)
}
