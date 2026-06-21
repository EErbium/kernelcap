package gpu

import (
	"fmt"
	"sync"

	"github.com/anomalyco/ai-compute-profiler/pkg/config"
	"github.com/anomalyco/ai-compute-profiler/pkg/model"
)

type GPUSampler struct {
	impl    gpuSamplerImpl
	mu      sync.RWMutex
	devices []model.GPUDeviceMetrics
	logf    func(format string, args ...any)
	avail   bool
}

type gpuSamplerImpl interface {
	init() error
	sample() ([]model.GPUDeviceMetrics, error)
	close()
}

var ErrGPUNotAvailable = fmt.Errorf("GPU monitoring not available")

func NewGPUSampler(cfg *config.Config, logf func(string, ...any)) *GPUSampler {
	s := &GPUSampler{
		logf:  logf,
		avail: true,
	}
	impl, err := newPlatformSampler()
	if err != nil {
		s.logf("gpu: platform sampler unavailable: %v", err)
		s.avail = false
		return s
	}
	s.impl = impl
	return s
}

func (s *GPUSampler) Run() error {
	if !s.avail || s.impl == nil {
		return ErrGPUNotAvailable
	}
	if err := s.impl.init(); err != nil {
		s.avail = false
		return fmt.Errorf("gpu: init: %w", err)
	}
	s.logf("gpu: sampler initialised")
	return nil
}

func (s *GPUSampler) Sample() {
	if !s.avail || s.impl == nil {
		return
	}
	devices, err := s.impl.sample()
	if err != nil {
		s.logf("gpu: sample error: %v", err)
		return
	}
	s.mu.Lock()
	s.devices = devices
	s.mu.Unlock()
}

func (s *GPUSampler) Available() bool {
	return s.avail
}

func (s *GPUSampler) LastGPUDevices() []model.GPUDeviceMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.GPUDeviceMetrics, len(s.devices))
	copy(out, s.devices)
	return out
}

func (s *GPUSampler) Close() {
	if s.impl != nil {
		s.impl.close()
	}
}
