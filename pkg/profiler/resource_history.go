package profiler

import (
	"time"
)

func newResourceHistory(key ResourceKey, maxSize int, maxAge time.Duration) *ResourceHistory {
	return &ResourceHistory{
		key:     key,
		points:  make([]TimeSeriesPoint, 0, maxSize),
		maxSize: maxSize,
		maxAge:  maxAge,
	}
}

func (rh *ResourceHistory) Append(p TimeSeriesPoint) {
	cutoff := p.Timestamp - int64(rh.maxAge.Seconds())
	trimmed := 0
	for i, pt := range rh.points {
		if pt.Timestamp >= cutoff {
			trimmed = i
			break
		}
		trimmed = i + 1
	}
	if trimmed > 0 {
		rh.points = rh.points[trimmed:]
	}

	if len(rh.points) >= rh.maxSize {
		excess := len(rh.points) + 1 - rh.maxSize
		if excess > 0 {
			rh.points = rh.points[excess:]
		}
	}

	rh.points = append(rh.points, p)
	rh.lastAccess = time.Now()
}

func (rh *ResourceHistory) Len() int {
	return len(rh.points)
}

func (rh *ResourceHistory) Points() []TimeSeriesPoint {
	out := make([]TimeSeriesPoint, len(rh.points))
	copy(out, rh.points)
	return out
}

func (rh *ResourceHistory) OldestTimestamp() int64 {
	if len(rh.points) == 0 {
		return 0
	}
	return rh.points[0].Timestamp
}

func (rh *ResourceHistory) NewestTimestamp() int64 {
	if len(rh.points) == 0 {
		return 0
	}
	maxTS := rh.points[0].Timestamp
	for _, p := range rh.points[1:] {
		if p.Timestamp > maxTS {
			maxTS = p.Timestamp
		}
	}
	return maxTS
}

func (rh *ResourceHistory) RollingAvgSMUtil() float64 {
	if len(rh.points) == 0 {
		return 0
	}
	var sum float64
	for _, p := range rh.points {
		sum += p.SMUtilPct
	}
	return sum / float64(len(rh.points))
}

func (rh *ResourceHistory) RollingAvgCPU() float64 {
	if len(rh.points) == 0 {
		return 0
	}
	var sum float64
	for _, p := range rh.points {
		sum += p.CPUUsagePct
	}
	return sum / float64(len(rh.points))
}

func (rh *ResourceHistory) MaxVRAMUsed() uint64 {
	var max uint64
	for _, p := range rh.points {
		if p.VRAMUsed > max {
			max = p.VRAMUsed
		}
	}
	return max
}

func (rh *ResourceHistory) LatestVRAMUsed() uint64 {
	if len(rh.points) == 0 {
		return 0
	}
	return rh.points[len(rh.points)-1].VRAMUsed
}

func (rh *ResourceHistory) LatestCPUUsage() float64 {
	if len(rh.points) == 0 {
		return 0
	}
	return rh.points[len(rh.points)-1].CPUUsagePct
}
