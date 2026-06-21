//go:build !linux

package rollback

func probeProcessState(pid int, procRoot string) RuntimeState {
	return RuntimeStateRunning
}
