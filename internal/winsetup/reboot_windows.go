package winsetup

import "golang.org/x/sys/windows/registry"

// rebootPending checks the standard Windows pending-reboot markers.
func rebootPending() bool {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
		registry.READ); err == nil {
		k.Close()
		return true
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if _, _, err := k.GetStringsValue("PendingFileRenameOperations"); err == nil {
			return true
		}
	}
	return false
}
