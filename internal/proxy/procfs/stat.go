package procfs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

var statBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 4096)
		return &b
	},
}

const cpuLinePrefix = "cpu "

type CPUStats struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOwait  uint64
	IRQ     uint64
	SoftIRQ uint64
	Steal   uint64
	Guest   uint64
}

type CPUUtilCalculator struct {
	prev      CPUStats
	clkTck    float64
	firstRead bool
}

func NewCPUUtilCalculator() *CPUUtilCalculator {
	return &CPUUtilCalculator{
		clkTck:    100.0,
		firstRead: true,
	}
}

func parseCPUStatLine(line []byte) (CPUStats, error) {
	fields := bytes.Fields(line)
	if len(fields) < 5 {
		return CPUStats{}, fmt.Errorf("malformed cpu line: %q", line)
	}
	var s CPUStats
	var err error
	parse := func(field []byte, name string, dest *uint64) {
		if err != nil {
			return
		}
		*dest, err = strconv.ParseUint(string(field), 10, 64)
		if err != nil {
			err = fmt.Errorf("parse %s field %q: %w", name, field, err)
		}
	}
	if len(fields) >= 2 {
		parse(fields[1], "user", &s.User)
	}
	if len(fields) >= 3 {
		parse(fields[2], "nice", &s.Nice)
	}
	if len(fields) >= 4 {
		parse(fields[3], "system", &s.System)
	}
	if len(fields) >= 5 {
		parse(fields[4], "idle", &s.Idle)
	}
	if len(fields) >= 6 {
		parse(fields[5], "iowait", &s.IOwait)
	}
	if len(fields) >= 7 {
		parse(fields[6], "irq", &s.IRQ)
	}
	if len(fields) >= 8 {
		parse(fields[7], "softirq", &s.SoftIRQ)
	}
	if len(fields) >= 9 {
		parse(fields[8], "steal", &s.Steal)
	}
	if len(fields) >= 10 {
		parse(fields[9], "guest", &s.Guest)
	}
	return s, err
}

func sumCPU(s CPUStats) uint64 {
	return s.User + s.Nice + s.System + s.Idle + s.IOwait +
		s.IRQ + s.SoftIRQ + s.Steal + s.Guest
}

func (c *CPUUtilCalculator) ReadAndCompute(procRoot string) (float64, error) {
	data := statBufPool.Get().(*[]byte)
	defer statBufPool.Put(data)

	f, err := os.Open(procRoot + "/stat")
	if err != nil {
		return 0, fmt.Errorf("open /proc/stat: %w", err)
	}
	defer f.Close()

	n, err := f.Read(*data)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("read /proc/stat: %w", err)
	}
	buf := (*data)[:n]

	var current CPUStats
	found := false
	lines := bytes.Split(buf, []byte("\n"))
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte(cpuLinePrefix)) {
			current, err = parseCPUStatLine(line)
			if err != nil {
				return 0, fmt.Errorf("parse /proc/stat cpu line: %w", err)
			}
			found = true
			break
		}
	}
	if !found {
		return 0, fmt.Errorf("no cpu line found in /proc/stat")
	}

	if c.firstRead {
		c.prev = current
		c.firstRead = false
		return 0, nil
	}

	prevTotal := sumCPU(c.prev)
	currTotal := sumCPU(current)
	totalDelta := currTotal - prevTotal
	if totalDelta == 0 {
		return 0, nil
	}

	idleDelta := current.Idle - c.prev.Idle
	iowaitDelta := current.IOwait - c.prev.IOwait

	util := 1.0 - float64(idleDelta+iowaitDelta)/float64(totalDelta)
	if util < 0 {
		util = 0
	}
	if util > 1.0 {
		util = 1.0
	}

	c.prev = current
	return util * 100.0, nil
}
