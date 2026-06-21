package procfs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

var procStatBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 8192)
		return &b
	},
}

var procStatusBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 4096)
		return &b
	},
}

type ProcMetrics struct {
	PID     int
	Comm    string
	UTime   uint64
	STime   uint64
	RSS     uint64
	VSize   uint64
}

type ProcDelta struct {
	PrevUTime uint64
	PrevSTime uint64
}

// ReadProcStat parses /proc/[pid]/stat with careful handling of the
// parenthesized comm field (field 2), which may contain spaces,
// parentheses, or other special characters.
func ReadProcStat(pid int, procRoot string) (*ProcMetrics, error) {
	path := fmt.Sprintf("%s/%d/stat", procRoot, pid)

	data := procStatBufPool.Get().(*[]byte)
	defer procStatBufPool.Put(data)

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	n, err := f.Read(*data)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	buf := (*data)[:n]

	return parseProcStat(buf, pid)
}

func parseProcStat(buf []byte, pid int) (*ProcMetrics, error) {
	rparen := bytes.LastIndexByte(buf, ')')
	if rparen < 0 {
		return nil, fmt.Errorf("no closing paren in /proc/%d/stat", pid)
	}

	lparen := bytes.IndexByte(buf, '(')
	if lparen < 0 || lparen >= rparen {
		return nil, fmt.Errorf("no opening paren in /proc/%d/stat", pid)
	}

	comm := string(buf[lparen+1 : rparen])
	rest := buf[rparen+2:]

	fields := bytes.Fields(rest)
	if len(fields) < 22 {
		return nil, fmt.Errorf("too few fields (%d) in /proc/%d/stat after comm", len(fields), pid)
	}

	var m ProcMetrics
	m.PID = pid
	m.Comm = comm

	var err error
	parseField := func(field []byte, name string, dest *uint64) {
		if err != nil {
			return
		}
		*dest, err = strconv.ParseUint(string(field), 10, 64)
		if err != nil {
			err = fmt.Errorf("parse %s in /proc/%d/stat: %w", name, pid, err)
		}
	}

	// After the comm field, fields from bytes.Fields(rest) are:
	//   [0] state, [1] ppid, [2] pgrp, [3] session, [4] tty_nr,
	//   [5] tpgid, [6] flags, [7] minflt, [8] cminflt, [9] majflt,
	//   [10] cmajflt, [11] utime, [12] stime, ...
	//   [20] vsize (field 23), [21] rss (field 24)
	parseField(fields[11], "utime", &m.UTime)
	parseField(fields[12], "stime", &m.STime)
	parseField(fields[20], "vsize", &m.VSize)
	parseField(fields[21], "rss_pages", &m.RSS)

	if err != nil {
		return nil, err
	}

	m.RSS *= 4096

	return &m, nil
}

// ReadProcStatus parses /proc/[pid]/status for VmRSS and VmSize.
// Used as a fallback / cross-check when /proc/[pid]/stat parsing is ambiguous.
func ReadProcStatus(pid int, procRoot string) (rssBytes, vsizeBytes uint64, err error) {
	path := fmt.Sprintf("%s/%d/status", procRoot, pid)

	data := procStatusBufPool.Get().(*[]byte)
	defer procStatusBufPool.Put(data)

	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	n, err := f.Read(*data)
	if err != nil && err != io.EOF {
		return 0, 0, fmt.Errorf("read %s: %w", path, err)
	}
	buf := (*data)[:n]

	return parseProcStatus(buf)
}

func parseProcStatus(buf []byte) (rssBytes, vsizeBytes uint64, err error) {
	lines := bytes.Split(buf, []byte("\n"))
	for _, line := range lines {
		if err != nil {
			return
		}
		if bytes.HasPrefix(line, []byte("VmRSS:")) {
			v, e := parseStatusValue(line)
			if e != nil {
				err = e
				return
			}
			rssBytes = v * 1024
		}
		if bytes.HasPrefix(line, []byte("VmSize:")) {
			v, e := parseStatusValue(line)
			if e != nil {
				err = e
				return
			}
			vsizeBytes = v * 1024
		}
	}
	return
}

func parseStatusValue(line []byte) (uint64, error) {
	colonIdx := bytes.IndexByte(line, ':')
	if colonIdx < 0 {
		return 0, fmt.Errorf("no colon in status line: %q", line)
	}
	rest := bytes.TrimSpace(line[colonIdx+1:])
	parts := bytes.Fields(rest)
	if len(parts) == 0 {
		return 0, fmt.Errorf("empty value in status line: %q", line)
	}
	val, err := strconv.ParseUint(string(parts[0]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse status value %q: %w", parts[0], err)
	}
	return val, nil
}

// ReadProcCmdline reads /proc/[pid]/cmdline for a more descriptive process name.
// Falls back to comm if cmdline is empty or unreadable.
func ReadProcCmdline(pid int, procRoot string) (string, error) {
	path := fmt.Sprintf("%s/%d/cmdline", procRoot, pid)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	data = bytes.ReplaceAll(data, []byte{0}, []byte(" "))
	cmdline := string(bytes.TrimSpace(data))
	if cmdline == "" {
		return "", fmt.Errorf("empty cmdline")
	}
	return cmdline, nil
}
