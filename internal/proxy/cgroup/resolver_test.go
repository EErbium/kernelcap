package cgroup

import (
	"testing"
)

func TestExtractContainerID(t *testing.T) {
	// 64-char hex Docker container ID
	dockerID := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	// 32-char hex containerd container ID
	containerdID := "abcdef1234567890abcdef1234567890"

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "docker systemd cgroup v2",
			data: "0::/system.slice/docker-" + dockerID + ".scope\n",
			want: dockerID,
		},
		{
			name: "docker cgroupfs cgroup v2",
			data: "0::/docker/" + dockerID + "\n",
			want: dockerID,
		},
		{
			name: "containerd systemd",
			data: "0::/system.slice/containerd-" + containerdID + ".scope\n",
			want: containerdID,
		},
		{
			name: "cri-containerd kubepods systemd",
			data: "0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-poda123b456c789.slice/cri-containerd-" + containerdID + ".scope\n",
			want: containerdID,
		},
		{
			name: "podman systemd",
			data: "0::/system.slice/libpod-" + dockerID + ".scope\n",
			want: dockerID,
		},
		{
			name: "bare metal process",
			data: "0::/\n",
			want: "",
		},
		{
			name: "non-container cgroup",
			data: "0::/system.slice/sshd.service\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractContainerID([]byte(tt.data))
			if got != tt.want {
				t.Errorf("extractContainerID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolverCache(t *testing.T) {
	r := NewResolver("/proc")
	cid := r.Resolve(999999999)
	_ = cid
	r.Evict(999999999)
}
