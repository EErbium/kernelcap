package cgroup

import (
	"testing"
)

func BenchmarkExtractContainerID(b *testing.B) {
	data := []byte("0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-poda123b456c789.slice/cri-containerd-abcdef1234567890abcdef1234567890.scope\n")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = extractContainerID(data)
	}
}

func BenchmarkExtractFromScope(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = containerIDFromScope("cri-containerd-abcdef1234567890abcdef1234567890.scope")
	}
}

func BenchmarkIsHex(b *testing.B) {
	s := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = isHex(s)
	}
}
