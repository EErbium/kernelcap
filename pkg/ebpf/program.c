// +build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

char __license[] SEC("license") = "Dual MIT/GPL";

#define MAX_COMM_LEN 16

struct event {
	__u32 pid;
	__u32 ppid;
	__u32 exit_code;
	char comm[MAX_COMM_LEN];
	__u8 event_type;
};

enum event_type {
	EVENT_EXEC = 1,
	EVENT_EXIT = 2,
};

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 32768);
	__type(key, __u32);
	__type(value, struct event);
} active_processes SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 16);
} events SEC(".maps");

static __always_inline int probe_exec(struct trace_event_raw_sched_process_exec *ctx)
{
	__u32 pid = bpf_get_current_pid_tgid() >> 32;
	__u32 ppid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
	struct event *existing;
	struct event ev = {};

	ev.pid = pid;
	ev.ppid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
	ev.event_type = EVENT_EXEC;
	bpf_get_current_comm(&ev.comm, sizeof(ev.comm));

	existing = bpf_map_lookup_elem(&active_processes, &pid);
	if (existing) {
		bpf_map_delete_elem(&active_processes, &pid);
	}

	bpf_map_update_elem(&active_processes, &pid, &ev, BPF_ANY);

	struct event *ring_ev = bpf_ringbuf_reserve(&events, sizeof(struct event), 0);
	if (!ring_ev) {
		return 0;
	}
	ring_ev->pid = ev.pid;
	ring_ev->ppid = ev.ppid;
	ring_ev->event_type = EVENT_EXEC;
	__builtin_memcpy(ring_ev->comm, ev.comm, sizeof(ev.comm));
	bpf_ringbuf_submit(ring_ev, 0);

	return 0;
}

static __always_inline int probe_exit(struct trace_event_raw_sched_process_exit *ctx)
{
	__u32 pid;
	bpf_probe_read_kernel(&pid, sizeof(pid), &ctx->pid);
	struct event ev = {};
	struct event *existing;

	existing = bpf_map_lookup_elem(&active_processes, &pid);
	if (existing) {
		ev = *existing;
		bpf_map_delete_elem(&active_processes, &pid);
	} else {
		ev.pid = pid;
		ev.ppid = 0;
		ev.event_type = EVENT_EXIT;
		bpf_get_current_comm(&ev.comm, sizeof(ev.comm));
	}

	bpf_probe_read_kernel(&ev.exit_code, sizeof(ev.exit_code), &ctx->exit_code);
	ev.event_type = EVENT_EXIT;

	struct event *ring_ev = bpf_ringbuf_reserve(&events, sizeof(struct event), 0);
	if (!ring_ev) {
		return 0;
	}
	ring_ev->pid = ev.pid;
	ring_ev->ppid = ev.ppid;
	ring_ev->exit_code = ev.exit_code;
	ring_ev->event_type = EVENT_EXIT;
	__builtin_memcpy(ring_ev->comm, ev.comm, sizeof(ev.comm));
	bpf_ringbuf_submit(ring_ev, 0);

	return 0;
}

SEC("tracepoint/sched/sched_process_exec")
int tracepoint_sched_process_exec(struct trace_event_raw_sched_process_exec *ctx)
{
	return probe_exec(ctx);
}

SEC("tracepoint/sched/sched_process_exit")
int tracepoint_sched_process_exit(struct trace_event_raw_sched_process_exit *ctx)
{
	return probe_exit(ctx);
}
