//go:build !linux

package mitigator

import "fmt"

func FreezeProcess(pid int) error {
	return fmt.Errorf("freeze pid %d: signal control not supported on this platform", pid)
}

func UnfreezeProcess(pid int) error {
	return fmt.Errorf("unfreeze pid %d: signal control not supported on this platform", pid)
}
