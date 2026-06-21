package procfs

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseCPUStatLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    CPUStats
		wantErr bool
	}{
		{
			name: "standard cpu line",
			line: "cpu  12345 678 9012 345678 111 222 333 444 555",
			want: CPUStats{
				User: 12345, Nice: 678, System: 9012, Idle: 345678,
				IOwait: 111, IRQ: 222, SoftIRQ: 333, Steal: 444, Guest: 555,
			},
		},
		{
			name: "short line",
			line: "cpu  12345 678 9012 345678",
			want: CPUStats{User: 12345, Nice: 678, System: 9012, Idle: 345678},
		},
		{
			name:    "empty",
			line:    "cpu",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCPUStatLine([]byte(tt.line))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseCPUStatLine() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("parseCPUStatLine() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadAndComputeCPU(t *testing.T) {
	dir := t.TempDir()
	procRoot := filepath.Join(dir, "proc")
	os.MkdirAll(procRoot, 0755)

	stat1 := `cpu  100 0 50 800 20 5 5 5 0
cpu0 100 0 50 800 20 5 5 5 0
intr 12345
ctxt 12345
`
	stat2 := `cpu  200 0 100 900 30 10 10 10 0
cpu0 200 0 100 900 30 10 10 10 0
intr 12345
ctxt 12345
`
	writeTestFile(t, procRoot, "stat", stat1)
	c := NewCPUUtilCalculator()
	got1, err := c.ReadAndCompute(procRoot)
	if err != nil {
		t.Fatalf("first read should return initial state (0): %v", err)
	}
	if got1 != 0 {
		t.Fatalf("first read should return 0, got %f", got1)
	}

	writeTestFile(t, procRoot, "stat", stat2)
	got2, err := c.ReadAndCompute(procRoot)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got2 <= 0 || got2 > 100 {
		t.Fatalf("unexpected cpu util: %f (expected between 0 and 100)", got2)
	}

	t.Logf("cpu utilization: %.2f%%", got2)
}

func TestReadMemoryInfo(t *testing.T) {
	dir := t.TempDir()
	procRoot := filepath.Join(dir, "proc")
	os.MkdirAll(procRoot, 0755)

	meminfo := `MemTotal:       32941268 kB
MemFree:         1875340 kB
MemAvailable:   25678900 kB
Buffers:          543212 kB
Cached:         23456789 kB
`
	writeTestFile(t, procRoot, "meminfo", meminfo)
	mem, err := ReadMemoryInfo(procRoot)
	if err != nil {
		t.Fatalf("ReadMemoryInfo: %v", err)
	}
	if mem.TotalBytes != 32941268*1024 {
		t.Fatalf("TotalBytes = %d, want %d", mem.TotalBytes, 32941268*1024)
	}
	if mem.UsedBytes != (32941268-25678900)*1024 {
		t.Fatalf("UsedBytes = %d, want %d", mem.UsedBytes, (32941268-25678900)*1024)
	}
}

func TestParseProcStat(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		pid     int
		want    *ProcMetrics
		wantErr bool
	}{
		{
			name: "simple process",
			data: "1234 (bash) S 1 1234 1234 34816 1234 4194304 1234 0 0 0 100 200 0 0 20 0 1 0 12345 12345678 1234 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			pid:  1234,
			want: &ProcMetrics{PID: 1234, Comm: "bash", UTime: 100, STime: 200, RSS: 1234 * 4096, VSize: 12345678},
		},
		{
			name: "process with spaces in comm",
			data: "5678 (java -Xmx2g) S 1 5678 5678 34816 1234 4194304 1234 0 0 0 50 75 0 0 20 0 1 0 12345 98765432 5678 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			pid:  5678,
			want: &ProcMetrics{PID: 5678, Comm: "java -Xmx2g", UTime: 50, STime: 75, RSS: 5678 * 4096, VSize: 98765432},
		},
		{
			name: "parentheses in comm",
			data: "9999 ((sd-pam)) S 1 9999 9999 34816 1234 4194304 1234 0 0 0 10 20 0 0 20 0 1 0 12345 1000000 9999 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			pid:  9999,
			want: &ProcMetrics{PID: 9999, Comm: "(sd-pam)", UTime: 10, STime: 20, RSS: 9999 * 4096, VSize: 1000000},
		},
		{
			name:    "invalid pid format",
			data:    "abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProcStat([]byte(tt.data), tt.pid)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseProcStat() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil || tt.wantErr {
				return
			}
			if got.Comm != tt.want.Comm {
				t.Errorf("Comm = %q, want %q", got.Comm, tt.want.Comm)
			}
			if got.UTime != tt.want.UTime {
				t.Errorf("UTime = %d, want %d", got.UTime, tt.want.UTime)
			}
			if got.STime != tt.want.STime {
				t.Errorf("STime = %d, want %d", got.STime, tt.want.STime)
			}
			if got.RSS != tt.want.RSS {
				t.Errorf("RSS = %d, want %d", got.RSS, tt.want.RSS)
			}
			if got.VSize != tt.want.VSize {
				t.Errorf("VSize = %d, want %d", got.VSize, tt.want.VSize)
			}
		})
	}
}

func TestParseProcStatus(t *testing.T) {
	status := `Name:   python3
Umask:  0022
State:  S (sleeping)
Tgid:   41029
Ngid:   0
Pid:    41029
PPid:   1
VmPeak:  25769803776 kB
VmSize:  25769803776 kB
VmLck:         0 kB
VmPin:         0 kB
VmHWM:  12884901888 kB
VmRSS:  12884901888 kB
RssAnon: 12884901888 kB
RssFile:           0 kB
RssShmem:          0 kB
VmData:  25769803776 kB
VmStk:         132 kB
VmExe:        123 kB
VmLib:       1452 kB
VmPTE:        145 kB
VmSwap:        0 kB
`
	rss, vsize, err := parseProcStatus([]byte(status))
	if err != nil {
		t.Fatalf("parseProcStatus: %v", err)
	}
	expectedRSS := uint64(12884901888) * 1024
	expectedVSize := uint64(25769803776) * 1024
	if rss != expectedRSS {
		t.Errorf("VmRSS = %d, want %d", rss, expectedRSS)
	}
	if vsize != expectedVSize {
		t.Errorf("VmSize = %d, want %d", vsize, expectedVSize)
	}
}
