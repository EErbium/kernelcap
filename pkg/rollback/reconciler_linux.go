//go:build linux

package rollback

import (
	"os"
	"path/filepath"
	"strings"
)

func probeProcessState(pid int, procRoot string) RuntimeState {
	procPath := filepath.Join(procRoot, itoa(pid))
	if _, err := os.Stat(procPath); os.IsNotExist(err) {
		return RuntimeStateNotFound
	}

	statusPath := filepath.Join(procPath, "status")
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return RuntimeStateRunning
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			state := strings.TrimSpace(line[6:])
			if len(state) > 0 && state[0] == 'T' {
				return RuntimeStateStopped
			}
			if len(state) > 0 && state[0] == 'Z' {
				return RuntimeStateZombie
			}
			return RuntimeStateRunning
		}
	}

	return RuntimeStateRunning
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 12)
	if n < 0 {
		n = -n
	}
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
