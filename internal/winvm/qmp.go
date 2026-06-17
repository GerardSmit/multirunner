package winvm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"time"
)

// QMPScreenshot connects to a QEMU QMP endpoint and writes a PNG screenshot of
// the guest display to outPath. Used to observe headless VM installs.
func QMPScreenshot(addr, outPath string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial qmp: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	br := bufio.NewReader(conn)
	if _, err := br.ReadString('\n'); err != nil { // greeting
		return fmt.Errorf("qmp greeting: %w", err)
	}
	send := func(v any) (string, error) {
		b, _ := json.Marshal(v)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return "", err
		}
		return br.ReadString('\n')
	}
	if _, err := send(map[string]any{"execute": "qmp_capabilities"}); err != nil {
		return fmt.Errorf("qmp capabilities: %w", err)
	}
	abs, _ := filepath.Abs(outPath)
	resp, err := send(map[string]any{
		"execute":   "screendump",
		"arguments": map[string]any{"filename": filepath.ToSlash(abs), "format": "png"},
	})
	if err != nil {
		return fmt.Errorf("screendump: %w", err)
	}
	if !contains(resp, "return") {
		return fmt.Errorf("screendump failed: %s", resp)
	}
	return nil
}

// QMPBootKeys resets the VM and sends Enter repeatedly to satisfy Windows
// media's "Press any key to boot from CD or DVD" prompt (which otherwise times
// out and falls through to PXE/no-boot). Used during bake.
func QMPBootKeys(addr string, presses int, every time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial qmp: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(presses+5) * every))

	br := bufio.NewReader(conn)
	if _, err := br.ReadString('\n'); err != nil {
		return fmt.Errorf("qmp greeting: %w", err)
	}
	send := func(v any) error {
		b, _ := json.Marshal(v)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return err
		}
		_, err := br.ReadString('\n')
		return err
	}
	if err := send(map[string]any{"execute": "qmp_capabilities"}); err != nil {
		return err
	}
	key := map[string]any{
		"execute":   "send-key",
		"arguments": map[string]any{"keys": []any{map[string]any{"type": "qcode", "data": "ret"}}},
	}
	for i := 0; i < presses; i++ {
		_ = send(key)
		time.Sleep(every)
	}
	return nil
}

// QMPEjectOnReset connects to QMP and waits for the guest's first RESET event
// (Windows Setup rebooting after it applies the image to disk), then ejects the
// install CD with block-backend id deviceID. This stops later reboots from
// re-entering the DVD UEFI loader — which triple-faults under WHPX ("Unexpected
// VP exit code 4") — sending them to the now-bootable HDD instead. Returns after
// the eject, or when timeout elapses with no reset.
func QMPEjectOnReset(addr, deviceID string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial qmp: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	br := bufio.NewReader(conn)
	if _, err := br.ReadString('\n'); err != nil {
		return fmt.Errorf("qmp greeting: %w", err)
	}
	if _, err := conn.Write([]byte(`{"execute":"qmp_capabilities"}` + "\n")); err != nil {
		return err
	}
	// Read until the first RESET event, then eject immediately (before post-reset
	// OVMF gets far enough to boot the DVD again).
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return fmt.Errorf("qmp read: %w", err)
		}
		if contains(line, `"event": "RESET"`) || contains(line, `"event":"RESET"`) {
			break
		}
	}
	eject, _ := json.Marshal(map[string]any{
		"execute":   "eject",
		"arguments": map[string]any{"device": deviceID, "force": true},
	})
	if _, err := conn.Write(append(eject, '\n')); err != nil {
		return err
	}
	_, _ = br.ReadString('\n') // eject response
	return nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
