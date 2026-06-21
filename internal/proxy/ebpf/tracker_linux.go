//go:build linux

package ebpf

import (
	"encoding/binary"
	"log"
	"os"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"github.com/cilium/ebpf/ringbuf"
)

var nativeEndian binary.ByteOrder

func init() {
	nativeEndian = detectEndian()
}

func detectEndian() binary.ByteOrder {
	var v uint32 = 0x01020304
	b := *(*[4]byte)(interface{}(&v)).([4]byte)
	if b[0] == 0x04 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

// linuxTracker is the real eBPF-based implementation for Linux.
// After running "go generate ./pkg/ebpf/", bpf2go produces
// tracker_bpfel.go / tracker_bpfeb.go which define trackerObjects
// and loadTrackerObjects used below.
type linuxTracker struct {
	objs       *trackerObjects
	execLink   link.Link
	exitLink   link.Link
	ringReader *ringbuf.Reader
	events     chan ProcessEvent
	logf       func(format string, args ...any)
}

var _ trackerImpl = (*linuxTracker)(nil)

func newPlatformTracker() (trackerImpl, error) {
	return &linuxTracker{
		events: make(chan ProcessEvent, 1024),
		logf:   log.Printf,
	}, nil
}

func (lt *linuxTracker) start() error {
	if err := rlimit.RemoveMemlock(); err != nil {
		lt.logf("ebpf: remove rlimit memlock (non-fatal): %v", err)
	}

	objs := &trackerObjects{}
	if err := loadTrackerObjects(objs, nil); err != nil {
		return err
	}
	lt.objs = objs

	var err error
	lt.execLink, err = link.Tracepoint("sched", "sched_process_exec", objs.TracepointSchedSchedProcessExec, nil)
	if err != nil {
		lt.cleanup()
		return err
	}

	lt.exitLink, err = link.Tracepoint("sched", "sched_process_exit", objs.TracepointSchedSchedProcessExit, nil)
	if err != nil {
		lt.cleanup()
		return err
	}

	lt.ringReader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		lt.cleanup()
		return err
	}

	go lt.pollRingBuffer()
	return nil
}

func (lt *linuxTracker) pollRingBuffer() {
	for {
		record, err := lt.ringReader.Read()
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			lt.logf("ebpf: ringbuf read error: %v", err)
			return
		}
		if len(record.RawSample) < 24 {
			continue
		}
		ev := ProcessEvent{
			PID:       nativeEndian.Uint32(record.RawSample[0:4]),
			PPID:      nativeEndian.Uint32(record.RawSample[4:8]),
			ExitCode:  nativeEndian.Uint32(record.RawSample[8:12]),
			EventType: record.RawSample[24],
		}
		copy(ev.Comm[:], record.RawSample[12:28])

		select {
		case lt.events <- ev:
		default:
		}
	}
}

func (lt *linuxTracker) close() error {
	lt.cleanup()
	return nil
}

func (lt *linuxTracker) cleanup() {
	if lt.ringReader != nil {
		lt.ringReader.Close()
	}
	if lt.execLink != nil {
		lt.execLink.Close()
	}
	if lt.exitLink != nil {
		lt.exitLink.Close()
	}
	if lt.objs != nil {
		lt.objs.Close()
	}
}

func (lt *linuxTracker) drain(maxEvents int) []ProcessEvent {
	var events []ProcessEvent
	for i := 0; i < maxEvents; i++ {
		select {
		case ev := <-lt.events:
			events = append(events, ev)
		default:
			return events
		}
	}
	return events
}

func (lt *linuxTracker) fallback() bool {
	return false
}
