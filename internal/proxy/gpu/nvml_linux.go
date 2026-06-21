//go:build linux

package gpu

import (
	"fmt"
	"math"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/model"
)

type platformSampler struct {
	mu      sync.Mutex
	devices []nvmlDevice
	initd   bool
}

type nvmlDevice struct {
	index       int
	handle      nvml.Device
	uuid        string
	model       string
	pciBusID    string
	hasMIG      bool
	migInstances []nvmlMigInstance
}

type nvmlMigInstance struct {
	index       int
	handle      nvml.Device
	gpuInstanceID uint32
	computeInstanceID uint32
}

var _ gpuSamplerImpl = (*platformSampler)(nil)

func newPlatformSampler() (gpuSamplerImpl, error) {
	return &platformSampler{}, nil
}

func (p *platformSampler) init() error {
	ret := nvml.Init()
	if ret == nvml.ERROR_LIBRARY_NOT_FOUND {
		return fmt.Errorf("libnvidia-ml.so not found: %w", ErrGPUNotAvailable)
	}
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvml.Init: %s", nvml.ErrorString(ret))
	}

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		nvml.Shutdown()
		return fmt.Errorf("nvml.DeviceGetCount: %s", nvml.ErrorString(ret))
	}

	for i := 0; i < count; i++ {
		dev, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}

		nd := nvmlDevice{index: i, handle: dev}

		uuid, ret := dev.GetUUID()
		if ret == nvml.SUCCESS {
			nd.uuid = uuid
		}

		name, ret := dev.GetName()
		if ret == nvml.SUCCESS {
			nd.model = name
		}

		pci, ret := dev.GetPciInfo()
		if ret == nvml.SUCCESS {
			nd.pciBusID = string(pci.BusId[:])
			nd.pciBusID = trimNull(nd.pciBusID)
		}

		nd.migInstances = enumerateMIGInstances(dev, i)
		nd.hasMIG = len(nd.migInstances) > 0

		p.devices = append(p.devices, nd)
	}

	p.initd = true
	return nil
}

func enumerateMIGInstances(dev nvml.Device, parentIndex int) []nvmlMigInstance {
	var instances []nvmlMigInstance

	migMode, ret := dev.GetMigMode()
	if ret != nvml.SUCCESS || migMode == 0 {
		return nil
	}

	gpuInstances, ret := nvml.DeviceGetGpuInstances(dev)
	if ret != nvml.SUCCESS || len(gpuInstances) == 0 {
		return nil
	}

	instanceIndex := 0
	for _, gi := range gpuInstances {
		info, ret := nvml.GpuInstanceGetInfo(gi)
		if ret != nvml.SUCCESS {
			continue
		}

		computeInstances, ret := nvml.GpuInstanceGetComputeInstances(gi)
		if ret != nvml.SUCCESS || len(computeInstances) == 0 {
			mi := nvmlMigInstance{
				index:          instanceIndex,
				handle:         info.Device,
				gpuInstanceID:  info.Id,
			}
			instances = append(instances, mi)
			instanceIndex++
			continue
		}

		for _, ci := range computeInstances {
			ciInfo, ret := nvml.ComputeInstanceGetInfo(ci)
			if ret != nvml.SUCCESS {
				continue
			}
			mi := nvmlMigInstance{
				index:             instanceIndex,
				handle:            info.Device,
				gpuInstanceID:     info.Id,
				computeInstanceID: ciInfo.Id,
			}
			instances = append(instances, mi)
			instanceIndex++
		}
	}

	return instances
}

func (p *platformSampler) sample() ([]model.GPUDeviceMetrics, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initd {
		return nil, fmt.Errorf("NVML not initialised")
	}

	var all []model.GPUDeviceMetrics
	globalIndex := 0

	for _, nd := range p.devices {
		if nd.hasMIG && len(nd.migInstances) > 0 {
			for _, mi := range nd.migInstances {
				m := collectDeviceMetrics(mi.handle, globalIndex, nd)
				m.MigDeviceGUID = fmt.Sprintf("MIG-%s/%d/%d",
					nd.uuid, mi.gpuInstanceID, mi.computeInstanceID)
				all = append(all, m)
				globalIndex++
			}
		} else {
			m := collectDeviceMetrics(nd.handle, globalIndex, nd)
			all = append(all, m)
			globalIndex++
		}
	}

	return all, nil
}

func collectDeviceMetrics(handle nvml.Device, globalIndex int, nd nvmlDevice) model.GPUDeviceMetrics {
	m := model.GPUDeviceMetrics{
		Index:    globalIndex,
		UUID:     nd.uuid,
		Model:    nd.model,
		PCIBusID: nd.pciBusID,
	}

	util, ret := handle.GetUtilizationRates()
	if ret == nvml.SUCCESS {
		m.SMUtilizationPct = float64(util.GPU)
		m.MemoryUtilizationPct = float64(util.Memory)
	}

	mem, ret := handle.GetMemoryInfo()
	if ret == nvml.SUCCESS {
		m.MemoryTotalBytes = mem.Total
		m.MemoryUsedBytes = mem.Used
		if mem.Total > 0 {
			m.MemoryUtilizationPct = math.Round(float64(mem.Used)/float64(mem.Total)*1000) / 10
		}
	}

	power, ret := handle.GetPowerUsage()
	if ret == nvml.SUCCESS {
		m.PowerDrawWatts = float64(power) / 1000.0
	}

	temp, ret := handle.GetTemperature(nvml.TEMPERATURE_GPU)
	if ret == nvml.SUCCESS {
		m.TemperatureCelsius = temp
	}

	gfxClock, ret := handle.GetClockInfo(nvml.CLOCK_GRAPHICS)
	if ret == nvml.SUCCESS {
		m.GraphicsClockMHz = gfxClock
	}

	memClock, ret := handle.GetClockInfo(nvml.CLOCK_MEM)
	if ret == nvml.SUCCESS {
		m.MemoryClockMHz = memClock
	}

	m.RunningProcesses = collectProcesses(handle)

	return m
}

func collectProcesses(handle nvml.Device) []model.GPUProcessMetrics {
	seen := make(map[uint32]uint64)

	computeProcs, ret := handle.GetComputeRunningProcesses()
	if ret == nvml.SUCCESS {
		for _, p := range computeProcs {
			if p.Pid == 0 {
				continue
			}
			if existing, ok := seen[p.Pid]; ok {
				if p.UsedGpuMemory > existing {
					seen[p.Pid] = p.UsedGpuMemory
				}
			} else {
				seen[p.Pid] = p.UsedGpuMemory
			}
		}
	}

	gfxProcs, ret := handle.GetGraphicsRunningProcesses()
	if ret == nvml.SUCCESS {
		for _, p := range gfxProcs {
			if p.Pid == 0 {
				continue
			}
			if existing, ok := seen[p.Pid]; ok {
				if p.UsedGpuMemory > existing {
					seen[p.Pid] = p.UsedGpuMemory
				}
			} else {
				seen[p.Pid] = p.UsedGpuMemory
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	procs := make([]model.GPUProcessMetrics, 0, len(seen))
	for pid, vram := range seen {
		procs = append(procs, model.GPUProcessMetrics{
			PID:           pid,
			VRAMUsedBytes: vram,
		})
	}
	return procs
}

func (p *platformSampler) close() {
	if p.initd {
		nvml.Shutdown()
		p.initd = false
	}
}

func trimNull(s string) string {
	for i, c := range s {
		if c == 0 {
			return s[:i]
		}
	}
	return s
}
