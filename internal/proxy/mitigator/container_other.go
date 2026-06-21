//go:build !linux

package mitigator

import (
	"context"
	"errors"
	"time"
)

var ErrContainerControlUnsupported = errors.New("container pause/unpause not supported on this platform")

type containerManager struct{}

func newContainerManager(_ string, _ bool, _ time.Duration, _, _ string) *containerManager {
	return &containerManager{}
}

func (cm *containerManager) Pause(_ context.Context, _ string) error {
	return ErrContainerControlUnsupported
}

func (cm *containerManager) Unpause(_ context.Context, _ string) error {
	return ErrContainerControlUnsupported
}
