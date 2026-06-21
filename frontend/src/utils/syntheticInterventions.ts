import type {
  ActiveInterventionNode,
  MitigationAction,
  MitigationStatus,
} from "../types/mitigation";

let idCounter = BigInt(Date.now());

function uid(prefix: string): string {
  idCounter += 1n;
  return `${prefix}_${idCounter.toString(36)}`;
}

const NODE_IDS = [
  "ip-10-0-1-42",
  "ip-10-0-1-77",
  "ip-10-0-2-103",
  "ip-10-0-2-204",
  "gpu-node-01",
  "gpu-node-02",
];

const PROCESS_NAMES = [
  "llama-inference",
  "sd-xl-worker",
  "gpt-batch-4o",
  "claude-api-bridge",
  "embedding-service",
  "whisper-transcribe",
  "mixtral-8x7b-serve",
  "flux-pro-gen",
];

const ACTIONS: MitigationAction[] = [
  "SIGSTOP_FREEZE",
  "CONTAINER_PAUSE",
  "API_REROUTE",
];

const VIOLATION_REASONS = [
  "SEMANTIC_REPETITION_LOOP detected — excessive identical payloads",
  "IDLE_GPU_HOG — SM utilization below 5% for 120s with >80% VRAM allocation",
  "HOST_MEMORY_LEAK — RSS growth rate 256MB/s over 60s window",
  "VELOCITY_CAP exceeded — 500+ requests/min on single PID",
  "PID_BOUNDARY violation — process crossed cgroup memory limit",
  "USER_WHITELIST override — process not in exempted list",
];



function pick<T>(arr: T[]): T {
  return arr[Math.floor(Math.random() * arr.length)];
}

function clampDate(secondsAgo: number): number {
  return Math.floor(Date.now() / 1000) - secondsAgo;
}

export function generateSyntheticIntervention(
  overrides?: Partial<ActiveInterventionNode>
): ActiveInterventionNode {
  const statusRoll = Math.random();
  let currentStatus: MitigationStatus;
  if (statusRoll < 0.45) currentStatus = "ACTIVE_ENFORCED";
  else if (statusRoll < 0.7) currentStatus = "RESOLVED";
  else if (statusRoll < 0.88) currentStatus = "ROLLBACK_FAILED";
  else currentStatus = "PENDING_VERIFICATION";

  const execSecondsAgo =
    currentStatus === "ACTIVE_ENFORCED" || currentStatus === "PENDING_VERIFICATION"
      ? Math.floor(Math.random() * 300) + 10
      : Math.floor(Math.random() * 3600) + 600;

  return {
    mitigationId: uid("mit"),
    nodeId: pick(NODE_IDS),
    targetPid: Math.floor(Math.random() * 60000) + 1000,
    processName: pick(PROCESS_NAMES),
    appliedAction: pick(ACTIONS),
    executionTimestamp: clampDate(execSecondsAgo),
    currentStatus,
    policyViolationReason: pick(VIOLATION_REASONS),
    ...overrides,
  };
}

export function generateSyntheticInterventions(
  count: number = 12
): ActiveInterventionNode[] {
  const result: ActiveInterventionNode[] = [];
  const usedPids = new Set<number>();
  for (let i = 0; i < count; i++) {
    let pid: number;
    do {
      pid = Math.floor(Math.random() * 60000) + 1000;
    } while (usedPids.has(pid));
    usedPids.add(pid);
    result.push(generateSyntheticIntervention({ targetPid: pid }));
  }
  return result;
}
