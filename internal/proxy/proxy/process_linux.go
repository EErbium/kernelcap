//go:build linux

package proxy

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type tcpEntry struct {
	localPort  uint16
	remotePort uint16
	remoteIP   string
	state      uint8
	inode      uint64
}

type linuxProcResolver struct {
	procRoot string
	inodePID map[uint64]int
}

var _ procResolverImpl = (*linuxProcResolver)(nil)

func newPlatformProcResolver() (procResolverImpl, error) {
	return &linuxProcResolver{
		procRoot: "/proc",
		inodePID: make(map[uint64]int),
	}, nil
}

func (l *linuxProcResolver) start() error {
	return nil
}

func (l *linuxProcResolver) refresh() error {
	newMap := make(map[uint64]int)

	entries, err := os.ReadDir(l.procRoot)
	if err != nil {
		return fmt.Errorf("read /proc: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		fdDir := fmt.Sprintf("%s/%d/fd", l.procRoot, pid)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(fmt.Sprintf("%s/%d/fd/%s", l.procRoot, pid, fd.Name()))
			if err != nil {
				continue
			}
			if !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode, err := strconv.ParseUint(link[8:len(link)-1], 10, 64)
			if err != nil || inode == 0 {
				continue
			}
			newMap[inode] = pid
		}
	}

	l.inodePID = newMap
	return nil
}

func (l *linuxProcResolver) resolveFromPort(localPort uint16, proxyPort int) (int, error) {
	entries, err := parseNetTCP(l.procRoot + "/net/tcp")
	if err != nil {
		return 0, err
	}

	entries6, err6 := parseNetTCP(l.procRoot + "/net/tcp6")
	if err6 != nil {
		entries6 = nil
	}
	all := append(entries, entries6...)

	for _, e := range all {
		if e.localPort != localPort {
			continue
		}
		if proxyPort > 0 && e.remotePort != uint16(proxyPort) {
			continue
		}
		if e.state != 1 {
			continue
		}

		if pid, ok := l.inodePID[e.inode]; ok {
			return pid, nil
		}
	}

	return 0, nil
}

func parseNetTCP(path string) ([]tcpEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []tcpEntry
	scanner := bufio.NewScanner(f)
	first := true

	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		// field 1: local_address (hex:hex)
		// field 2: remote_address (hex:hex)
		// field 3: state (hex)
		// field 9: inode

		localParts := strings.Split(fields[1], ":")
		if len(localParts) < 2 {
			continue
		}
		localPort, err := strconv.ParseUint(localParts[len(localParts)-1], 16, 16)
		if err != nil {
			continue
		}

		remoteParts := strings.Split(fields[2], ":")
		if len(remoteParts) < 2 {
			continue
		}
		remotePort, err := strconv.ParseUint(remoteParts[len(remoteParts)-1], 16, 16)
		if err != nil {
			continue
		}

		state, err := strconv.ParseUint(fields[3], 16, 8)
		if err != nil {
			continue
		}

		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}

		entries = append(entries, tcpEntry{
			localPort:  uint16(localPort),
			remotePort: uint16(remotePort),
			state:      uint8(state),
			inode:      inode,
		})
	}

	return entries, scanner.Err()
}
