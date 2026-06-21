//go:build linux

package mitigator

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func FreezeProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("freeze: find pid %d: %w", pid, err)
	}
	if err := p.Signal(syscall.SIGSTOP); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("freeze pid %d: %w", pid, ErrProcessAlreadyExited)
		}
		if errno, ok := err.(syscall.Errno); ok {
			switch errno {
			case syscall.EPERM:
				return fmt.Errorf("freeze pid %d: %w", pid, ErrPermissionDenied)
			case syscall.ESRCH:
				return fmt.Errorf("freeze pid %d: %w", pid, ErrProcessNotExist)
			}
		}
		return fmt.Errorf("freeze pid %d: %w", pid, err)
	}
	return nil
}

func UnfreezeProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("unfreeze: find pid %d: %w", pid, err)
	}
	if err := p.Signal(syscall.SIGCONT); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("unfreeze pid %d: %w", pid, ErrProcessAlreadyExited)
		}
		if errno, ok := err.(syscall.Errno); ok {
			switch errno {
			case syscall.EPERM:
				return fmt.Errorf("unfreeze pid %d: %w", pid, ErrPermissionDenied)
			case syscall.ESRCH:
				return fmt.Errorf("unfreeze pid %d: %w", pid, ErrProcessNotExist)
			}
		}
		return fmt.Errorf("unfreeze pid %d: %w", pid, err)
	}
	return nil
}
