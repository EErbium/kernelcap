//go:build linux

package mitigator

import (
	"bytes"
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/client"
)

type containerManager struct {
	dockerEnabled bool
	dockerOnce    sync.Once
	dockerClient  *client.Client
	dockerHost    string
	dockerTimeout time.Duration
	cgroupRoot    string
	procRoot      string
}

func newContainerManager(dockerHost string, dockerEnabled bool, dockerTimeout time.Duration, cgroupRoot, procRoot string) *containerManager {
	return &containerManager{
		dockerEnabled: dockerEnabled,
		dockerHost:    dockerHost,
		dockerTimeout: dockerTimeout,
		cgroupRoot:    cgroupRoot,
		procRoot:      procRoot,
	}
}

func (cm *containerManager) docker(ctx context.Context) (*client.Client, error) {
	var initErr error
	cm.dockerOnce.Do(func() {
		cli, err := client.NewClientWithOpts(
			client.WithHost(cm.dockerHost),
			client.WithAPIVersionNegotiation(),
			client.WithTimeout(cm.dockerTimeout),
		)
		if err != nil {
			initErr = fmt.Errorf("docker client init: %w", err)
			return
		}
		if _, err := cli.Ping(ctx); err != nil {
			cli.Close()
			initErr = fmt.Errorf("docker ping: %w", err)
			return
		}
		cm.dockerClient = cli
	})
	return cm.dockerClient, initErr
}

func (cm *containerManager) Pause(ctx context.Context, containerID string) error {
	if cm.dockerEnabled {
		cli, err := cm.docker(ctx)
		if err == nil && cli != nil {
			if pErr := cli.ContainerPause(ctx, containerID); pErr == nil {
				return nil
			}
		}
	}
	return cm.cgroupFreeze(containerID, true)
}

func (cm *containerManager) Unpause(ctx context.Context, containerID string) error {
	if cm.dockerEnabled {
		cli, err := cm.docker(ctx)
		if err == nil && cli != nil {
			if uErr := cli.ContainerUnpause(ctx, containerID); uErr == nil {
				return nil
			}
		}
	}
	return cm.cgroupFreeze(containerID, false)
}

func (cm *containerManager) cgroupFreeze(containerID string, freeze bool) error {
	path, err := cm.resolveCgroupPath(containerID)
	if err != nil {
		return fmt.Errorf("cgroup freeze: %w", err)
	}
	freezePath := filepath.Join(cm.cgroupRoot, path, "cgroup.freeze")

	val := "0"
	if freeze {
		val = "1"
	}
	if err := os.WriteFile(freezePath, []byte(val), 0644); err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("cgroup freeze write %s: %w", freezePath, ErrPermissionDenied)
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("cgroup freeze path %s: %w", freezePath, ErrProcessNotExist)
		}
		return fmt.Errorf("cgroup freeze write %s: %w", freezePath, err)
	}
	return nil
}

func (cm *containerManager) resolveCgroupPath(containerID string) (string, error) {
	entries, err := os.ReadDir(cm.procRoot)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", cm.procRoot, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := entry.Name()
		if pid[0] < '0' || pid[0] > '9' {
			continue
		}
		cgroupPath := filepath.Join(cm.procRoot, pid, "cgroup")
		data, err := os.ReadFile(cgroupPath)
		if err != nil {
			continue
		}
		if cgroupV2Path := matchCgroupPath(data, containerID); cgroupV2Path != "" {
			return cgroupV2Path, nil
		}
	}
	return "", fmt.Errorf("container %s: cgroup path not found", containerID)
}

func matchCgroupPath(data []byte, containerID string) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(line, containerID) {
			continue
		}
		parts := strings.SplitN(line, "::", 2)
		if len(parts) == 2 {
			path := strings.TrimSpace(parts[1])
			if path != "" && path[0] == '/' {
				return path[1:]
			}
			return path
		}
		idx := strings.LastIndexByte(line, ':')
		if idx < 0 {
			continue
		}
		path := strings.TrimSpace(line[idx+1:])
		if path != "" && path[0] == '/' {
			return path[1:]
		}
		return path
	}
	return ""
}


