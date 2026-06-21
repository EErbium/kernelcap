//go:build !linux

package ebpf

import "fmt"

// stubTracker is a no-op implementation for non-Linux platforms.
// eBPF is only available on Linux, so this tracker always reports
// fallback mode, causing the collector to use /proc scanning only.
type stubTracker struct{}

var _ trackerImpl = (*stubTracker)(nil)

func newPlatformTracker() (trackerImpl, error) {
	return &stubTracker{}, nil
}

func (st *stubTracker) start() error {
	return fmt.Errorf("eBPF is only available on Linux")
}

func (st *stubTracker) close() error {
	return nil
}

func (st *stubTracker) drain(maxEvents int) []ProcessEvent {
	return nil
}

func (st *stubTracker) fallback() bool {
	return true
}
