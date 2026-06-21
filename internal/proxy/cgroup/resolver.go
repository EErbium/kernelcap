package cgroup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

var cgroupBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 1024)
		return &b
	},
}

type Resolver struct {
	procRoot string
	cache    map[int]string
}

func NewResolver(procRoot string) *Resolver {
	return &Resolver{
		procRoot: procRoot,
		cache:    make(map[int]string),
	}
}

func (r *Resolver) Resolve(pid int) string {
	if cached, ok := r.cache[pid]; ok {
		return cached
	}
	cid, err := readCgroupContainerID(pid, r.procRoot)
	if err != nil {
		return ""
	}
	if len(cid) > 0 {
		r.cache[pid] = cid
	}
	return cid
}

func (r *Resolver) Evict(pid int) {
	delete(r.cache, pid)
}

func readCgroupContainerID(pid int, procRoot string) (string, error) {
	path := fmt.Sprintf("%s/%d/cgroup", procRoot, pid)
	data := cgroupBufPool.Get().(*[]byte)
	defer cgroupBufPool.Put(data)

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	n, err := f.Read(*data)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	buf := (*data)[:n]

	return extractContainerID(buf), nil
}

func extractContainerID(cgroupData []byte) string {
	line := cgroupData
	if nl := bytes.IndexByte(cgroupData, '\n'); nl >= 0 {
		line = cgroupData[:nl]
	}
	line = bytes.TrimSpace(line)

	colonParts := bytes.Split(line, []byte(":"))
	if len(colonParts) < 3 {
		return ""
	}
	path := bytes.TrimSpace(colonParts[2])
	if len(path) == 0 || path[0] != '/' {
		return ""
	}

	segments := bytes.Split(path, []byte("/"))
	best := ""

	for _, seg := range segments {
		s := string(seg)
		if s == "" || s == "system.slice" || s == "kubepods.slice" || s == "docker" {
			continue
		}

		if s == ".scope" || s == ".slice" {
			continue
		}

		if strings.HasSuffix(s, ".scope") {
			if id := containerIDFromScope(s); id != "" {
				if len(id) > len(best) {
					best = id
				}
			}
			continue
		}

		if strings.HasSuffix(s, ".slice") {
			if id := containerIDFromSlice(s); id != "" {
				if len(id) > len(best) {
					best = id
				}
			}
			continue
		}

		if len(s) >= 32 && isHex(s) {
			return s
		}
	}

	return best
}

func isHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func containerIDFromScope(seg string) string {
	if !strings.HasSuffix(seg, ".scope") {
		return ""
	}
	base := seg[:len(seg)-6]

	// Try to find something that looks like a container ID after the last hyphen.
	// docker-<sha256>.scope -> sha256
	// containerd-<ns>-<id>.scope -> id (last hex segment after hyphen)
	// libpod-<sha256>.scope -> sha256
	lastHyphen := strings.LastIndexByte(base, '-')
	if lastHyphen >= 0 {
		candidate := base[lastHyphen+1:]
		if isHex(candidate) && len(candidate) >= 8 {
			return candidate
		}
		// Check second-to-last hyphen (for containerd-<ns>-<id> pattern)
		prevHyphen := strings.LastIndexByte(base[:lastHyphen], '-')
		if prevHyphen >= 0 {
			candidate = base[prevHyphen+1:]
			if isHex(candidate) && len(candidate) >= 8 {
				return candidate
			}
		}
	}

	if isHex(base) && len(base) >= 8 {
		return base
	}

	return ""
}

func containerIDFromSlice(seg string) string {
	if !strings.HasSuffix(seg, ".slice") {
		return ""
	}
	base := seg[:len(seg)-6]

	// kubepods-burstable-pod<uid>.slice -> poduid
	if strings.Contains(base, "pod") {
		podIdx := strings.LastIndex(base, "pod")
		if podIdx >= 0 && podIdx+3 < len(base) {
			return base[podIdx+3:]
		}
	}

	// Try last-hyphen heuristic
	lastHyphen := strings.LastIndexByte(base, '-')
	if lastHyphen >= 0 {
		candidate := base[lastHyphen+1:]
		if isHex(candidate) && len(candidate) >= 8 {
			return candidate
		}
	}

	return ""
}
