package pipeline

import (
	"sync"
)

type RingBuffer struct {
	mu       sync.Mutex
	entries  [][]byte
	writeIdx int
	count    int
	curBytes int
	maxBytes int
}

const DefaultRingBufferMaxBytes = 50 * 1024 * 1024

func NewRingBuffer(maxBytes int) *RingBuffer {
	if maxBytes <= 0 {
		maxBytes = DefaultRingBufferMaxBytes
	}
	return &RingBuffer{
		entries:  make([][]byte, 0, 1024),
		maxBytes: maxBytes,
	}
}

func (rb *RingBuffer) Push(data []byte) {
	if len(data) == 0 {
		return
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	dataLen := len(data)
	for rb.curBytes+dataLen > rb.maxBytes && rb.count > 0 {
		rb.popLocked()
	}

	rb.entries = append(rb.entries, data)
	rb.curBytes += dataLen
	rb.count++
}

func (rb *RingBuffer) Pop() ([]byte, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil, false
	}
	return rb.popLocked(), true
}

func (rb *RingBuffer) popLocked() []byte {
	if rb.count == 0 {
		return nil
	}
	entry := rb.entries[0]
	rb.entries[0] = nil
	rb.entries = rb.entries[1:]
	rb.curBytes -= len(entry)
	rb.count--
	return entry
}

func (rb *RingBuffer) Drain(maxEntries int) [][]byte {
	if maxEntries <= 0 {
		maxEntries = 100
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil
	}

	n := maxEntries
	if n > rb.count {
		n = rb.count
	}

	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		out[i] = rb.entries[0]
		rb.entries[0] = nil
		rb.entries = rb.entries[1:]
		rb.curBytes -= len(out[i])
		rb.count--
	}
	return out
}

func (rb *RingBuffer) Peek() ([]byte, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil, false
	}
	return rb.entries[0], true
}

func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

func (rb *RingBuffer) Bytes() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.curBytes
}

func (rb *RingBuffer) Capacity() int {
	return rb.maxBytes
}

func (rb *RingBuffer) Close() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for i := range rb.entries {
		rb.entries[i] = nil
	}
	rb.entries = nil
	rb.count = 0
	rb.curBytes = 0
}

func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for i := range rb.entries {
		rb.entries[i] = nil
	}
	rb.entries = rb.entries[:0]
	rb.count = 0
	rb.curBytes = 0
	rb.writeIdx = 0
}
