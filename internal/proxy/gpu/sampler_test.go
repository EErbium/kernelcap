package gpu

import (
	"errors"
	"testing"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/config"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
)

func TestNewGPUSampler(t *testing.T) {
	cfg := config.DefaultConfig()
	logf := func(format string, args ...any) {
		t.Logf(format, args...)
	}
	s := NewGPUSampler(cfg, logf)
	if s == nil {
		t.Fatal("NewGPUSampler returned nil")
	}

	err := s.Run()
	if errors.Is(err, ErrGPUNotAvailable) {
		t.Log("GPU sampler not available (expected on non-Linux or without NVIDIA GPU)")
	} else if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	} else {
		t.Log("GPU sampler initialised successfully")
	}

	if !s.Available() {
		t.Log("Sampler correctly reports unavailable after failed init")
	}

	devices := s.LastGPUDevices()
	if devices == nil {
		t.Log("LastGPUDevices returned nil (expected when not available)")
	} else {
		t.Logf("Got %d GPU devices", len(devices))
	}

	s.Close()
}

func TestLastGPUDevicesEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewGPUSampler(cfg, nil)
	_ = s.Run()

	devices := s.LastGPUDevices()
	if devices == nil {
		t.Log("devices is nil as expected")
	} else if len(devices) == 0 {
		t.Log("devices is empty as expected")
	}
}

func TestGPUSnapshotModel(t *testing.T) {
	snap := model.Snapshot{
		GPUDevices: []model.GPUDeviceMetrics{
			{
				Index:                0,
				UUID:                 "GPU-test-uuid",
				Model:                "NVIDIA Test GPU",
				SMUtilizationPct:     42.5,
				MemoryUtilizationPct: 33.3,
				MemoryTotalBytes:     85899345920,
				MemoryUsedBytes:      42949672960,
				PowerDrawWatts:       150.0,
				TemperatureCelsius:   65,
				GraphicsClockMHz:     1755,
				MemoryClockMHz:       1593,
				RunningProcesses: []model.GPUProcessMetrics{
					{PID: 12345, VRAMUsedBytes: 21474836480},
				},
			},
		},
	}

	data, err := snap.JSON()
	if err != nil {
		t.Fatalf("JSON marshaling failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("JSON output is empty")
	}
	t.Logf("GPU snapshot JSON: %s", string(data))
}
