//go:build !windows

package backend

// autoIsolation on non-Windows hosts is only reached if a containerd Windows
// backend is somehow constructed off-Windows; default to the safe (client) mode.
func autoIsolation() string { return "hyperv" }
