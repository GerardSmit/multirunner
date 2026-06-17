//go:build !windows

package winsetup

func rebootPending() bool { return false }
