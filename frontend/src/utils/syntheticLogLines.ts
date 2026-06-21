import type { RealTimeLogLine, LogLevel } from "../types/logging";

let idCounter = BigInt(Date.now());


function uid(): string {
  idCounter += 1n;
  return `log_${idCounter.toString(36)}`;
}

const NODE_IDS = [
  "ip-10-0-1-42",
  "ip-10-0-1-77",
  "ip-10-0-2-103",
  "gpu-node-01",
  "gpu-node-02",
  "control-plane-0",
];

const MESSAGES: Record<LogLevel, string[]> = {
  DEBUG: [
    "nvidia-smi poll complete \u2014 4 devices detected",
    "ringbuf consumption rate 142 msg/s",
    "procfs scan: 312 active file descriptors",
    "\x1b[90mcgroup memory.stat parsed in 2.3ms\x1b[0m",
    "nvml health check: GPU 0 temp 62\u00b0C, GPU 1 temp 58\u00b0C",
    "ebpf kprobe attached to tcp_sendmsg",
    "delta since last poll: 1998ms (expected 2000ms)",
    "token bucket capacity: 974 / 1000",
  ],
  INFO: [
    "Mitigation resolved for PID 4421 \u2014 action: CONTAINER_PAUSE lifted",
    "Webhook dispatch to https://hooks.example.com/alerts returned 200",
    "Policy evaluation passed \u2014 no matching restrictions",
    "Rollback reconciliation complete: 3 drift corrections applied",
    "Agent handshake OK \u2014 upstream endpoint reachable",
    "\x1b[32mConnection pool refreshed \u2014 12 idle connections\x1b[0m",
    "Ingestion batch flushed: 24 metrics, 1 alert",
  ],
  WARN: [
    "Token velocity approaching cap (482/min, limit 500)",
    "GPU mem reclaim delay exceeded threshold (240ms > 200ms)",
    "\x1b[33mPolicy velocity cap hit for PID 8832 \u2014 throttling\x1b[0m",
    "Memory trend OLS slope +128MB/s \u2014 monitoring",
    "Detector similarity index at 0.89 \u2014 approaching threshold",
    "Webhook dispatch queue depth: 47 pending",
  ],
  CRITICAL: [
    "\x1b[1;31mALERT: SEMANTIC_REPETITION_LOOP detected on llama-inference (PID 12904)\x1b[0m",
    "\x1b[1;31mALERT: IDLE_GPU_HOG \u2014 SM < 3% for 180s with 94% VRAM allocation\x1b[0m",
    "\x1b[1;31mACTION: SIGSTOP sent to PID 12904 (llama-inference)\x1b[0m",
    "\x1b[1;31mHOST_MEMORY_LEAK: RSS grew 2.4GB in 60s on PID 7712\x1b[0m",
    "\x1b[1;31mMitigation FAILED for PID 12904 \u2014 signal delivery error EPERM\x1b[0m",
  ],
};

const WEIGHTS: { level: LogLevel; weight: number }[] = [
  { level: "DEBUG", weight: 40 },
  { level: "INFO", weight: 35 },
  { level: "WARN", weight: 18 },
  { level: "CRITICAL", weight: 7 },
];

function pickWeightedLevel(): LogLevel {
  const total = WEIGHTS.reduce((s, w) => s + w.weight, 0);
  let r = Math.random() * total;
  for (const entry of WEIGHTS) {
    r -= entry.weight;
    if (r <= 0) return entry.level;
  }
  return "INFO";
}

export function generateSyntheticLogLine(): RealTimeLogLine {
  const level = pickWeightedLevel();
  const msgs = MESSAGES[level];
  const now = Math.floor(Date.now() / 1000);
  return {
    id: uid(),
    timestamp: now,
    originNodeId: NODE_IDS[Math.floor(Math.random() * NODE_IDS.length)],
    logLevel: level,
    messagePayload: msgs[Math.floor(Math.random() * msgs.length)],
  };
}

export function createSyntheticLogSource(
  onLine: (line: RealTimeLogLine) => void,
  intervalMs: number = 400
): { start: () => void; stop: () => void } {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let running = false;

  function tick() {
    if (!running) return;
    onLine(generateSyntheticLogLine());
    const jitter = Math.random() * intervalMs * 0.4;
    timer = setTimeout(tick, intervalMs + jitter);
  }

  function start() {
      if (running) return;
      running = true;
      tick();
    }

  function stop() {
      running = false;
      if (timer !== null) {
        clearTimeout(timer);
        timer = null;
      }
    }

  return {
    start,
    stop,
  };
}
