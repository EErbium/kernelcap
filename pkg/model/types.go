package model

import (
	"encoding/json"
	"fmt"
	"time"
)

type HostMetrics struct {
	CPUUtilizationPct float64 `json:"cpu_utilization_pct"`
	MemoryTotalBytes  uint64  `json:"memory_total_bytes"`
	MemoryUsedBytes   uint64  `json:"memory_used_bytes"`
}

type ProcessMetrics struct {
	PID         int     `json:"pid"`
	ContainerID string  `json:"container_id,omitempty"`
	ProcessName string  `json:"process_name"`
	CPUUsagePct float64 `json:"cpu_usage_pct"`
	RSSBytes    uint64  `json:"rss_bytes"`
	VSizeBytes  uint64  `json:"vsize_bytes"`
}

type GPUProcessMetrics struct {
	PID           uint32 `json:"pid"`
	VRAMUsedBytes uint64 `json:"vram_used_bytes"`
}

type GPUDeviceMetrics struct {
	Index                uint                `json:"index"`
	UUID                 string              `json:"uuid"`
	Model                string              `json:"model"`
	PCIBusID             string              `json:"pci_bus_id,omitempty"`
	MigDeviceGUID        string              `json:"mig_device_guid,omitempty"`
	SMUtilizationPct     float64             `json:"sm_utilization_pct"`
	MemoryUtilizationPct float64             `json:"memory_utilization_pct"`
	MemoryTotalBytes     uint64              `json:"memory_total_bytes"`
	MemoryUsedBytes      uint64              `json:"memory_used_bytes"`
	PowerDrawWatts       float64             `json:"power_draw_watts"`
	TemperatureCelsius   uint32              `json:"temperature_celsius"`
	GraphicsClockMHz     uint32              `json:"graphics_clock_mhz"`
	MemoryClockMHz       uint32              `json:"memory_clock_mhz"`
	RunningProcesses     []GPUProcessMetrics `json:"running_compute_processes,omitempty"`
}

type Snapshot struct {
	Timestamp          int64               `json:"timestamp"`
	HostMetrics        HostMetrics         `json:"host_metrics"`
	MonitoredProcesses []ProcessMetrics    `json:"monitored_processes"`
	GPUDevices         []GPUDeviceMetrics  `json:"gpu_devices,omitempty"`
}

func (s *Snapshot) JSON() ([]byte, error) {
	s.Timestamp = time.Now().Unix()
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	return data, nil
}

func (s *Snapshot) JSONIndent() ([]byte, error) {
	s.Timestamp = time.Now().Unix()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	return data, nil
}
