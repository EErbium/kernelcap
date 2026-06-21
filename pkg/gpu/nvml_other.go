//go:build !linux

package gpu

import (
	"github.com/anomalyco/ai-compute-profiler/pkg/model"
)

type platformSampler struct{}

var _ gpuSamplerImpl = (*platformSampler)(nil)

func newPlatformSampler() (gpuSamplerImpl, error) {
	return &platformSampler{}, nil
}

func (p *platformSampler) init() error {
	return ErrGPUNotAvailable
}

func (p *platformSampler) sample() ([]model.GPUDeviceMetrics, error) {
	return nil, ErrGPUNotAvailable
}

func (p *platformSampler) close() {}
