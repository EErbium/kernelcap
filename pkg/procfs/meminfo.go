package procfs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

var meminfoBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 4096)
		return &b
	},
}

type MemoryInfo struct {
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
}

func ReadMemoryInfo(procRoot string) (*MemoryInfo, error) {
	data := meminfoBufPool.Get().(*[]byte)
	defer meminfoBufPool.Put(data)

	f, err := os.Open(procRoot + "/meminfo")
	if err != nil {
		return nil, fmt.Errorf("open /proc/meminfo: %w", err)
	}
	defer f.Close()

	n, err := f.Read(*data)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	buf := (*data)[:n]

	var totalKB, availKB uint64
	foundTotal, foundAvail := false, false

	lines := bytes.Split(buf, []byte("\n"))
	for _, line := range lines {
		if !foundTotal && bytes.HasPrefix(line, []byte("MemTotal:")) {
			totalKB, err = parseMeminfoValue(line)
			if err != nil {
				return nil, fmt.Errorf("parse MemTotal: %w", err)
			}
			foundTotal = true
		}
		if !foundAvail && bytes.HasPrefix(line, []byte("MemAvailable:")) {
			availKB, err = parseMeminfoValue(line)
			if err != nil {
				return nil, fmt.Errorf("parse MemAvailable: %w", err)
			}
			foundAvail = true
		}
		if foundTotal && foundAvail {
			break
		}
	}

	if !foundTotal {
		return nil, fmt.Errorf("MemTotal not found in /proc/meminfo")
	}
	if !foundAvail {
		availKB = totalKB
	}

	totalBytes := totalKB * 1024
	availBytes := availKB * 1024
	usedBytes := totalBytes - availBytes

	return &MemoryInfo{
		TotalBytes:     totalBytes,
		AvailableBytes: availBytes,
		UsedBytes:      usedBytes,
	}, nil
}

func parseMeminfoValue(line []byte) (uint64, error) {
	colonIdx := bytes.IndexByte(line, ':')
	if colonIdx < 0 {
		return 0, fmt.Errorf("no colon in meminfo line: %q", line)
	}
	rest := bytes.TrimSpace(line[colonIdx+1:])
	parts := bytes.Fields(rest)
	if len(parts) == 0 {
		return 0, fmt.Errorf("empty value in meminfo line: %q", line)
	}
	val, err := strconv.ParseUint(string(parts[0]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse meminfo value %q: %w", parts[0], err)
	}
	return val, nil
}
