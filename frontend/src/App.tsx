import { useState, useEffect, useRef } from "react";
import { MetricsDashboard } from "./components/MetricsDashboard";
import { MitigationConsole } from "./components/MitigationConsole";
import { useSystemStream } from "./hooks/useSystemStream";
import { useMetricsStore } from "./hooks/useMetricsStore";
import { useMitigationStore } from "./hooks/useMitigationStore";
import { getStoredUserProfile } from "./utils/jwtProfile";
import { SystemLogTerminal } from "./components/SystemLogTerminal";
import { createSyntheticLogSource } from "./utils/syntheticLogLines";
import type { ConsolidatedAlert, ProxyProcessMetrics } from "./types/metrics";
import type { MitigationAction, ActiveInterventionNode } from "./types/mitigation";
import type { RealTimeLogLine } from "./types/logging";

const SSE_URL = "/api/v2/events/stream";
const AUTH_TOKEN = import.meta.env.VITE_API_TOKEN ?? "";
const userProfile = getStoredUserProfile();


type TabId = "dashboard" | "mitigation" | "logs";

function getStr(obj: Record<string, unknown>, ...keys: string[]): string {
  for (const k of keys) {
    const v = obj[k];
    if (typeof v === "string") return v;
  }
  return "";
}

function getNum(obj: Record<string, unknown>, ...keys: string[]): number {
  for (const k of keys) {
    const v = obj[k];
    if (typeof v === "number") return v;
  }
  return 0;
}

const ANOMALY_MAP: Record<string, ProxyProcessMetrics["anomalyStatus"]> = {
  SEMANTIC_REPETITION_LOOP: "SEMANTIC_LOOP",
  IDLE_GPU_HOG: "IDLE_HOG",
  HOST_MEMORY_LEAK: "IDLE_HOG",
};

function normalizeAnomalyType(raw: string): ProxyProcessMetrics["anomalyStatus"] {
  return ANOMALY_MAP[raw] ?? "HEALTHY";
}

const ALERT_TO_ACTION: Record<string, MitigationAction> = {
  SEMANTIC_REPETITION_LOOP: "SIGSTOP_FREEZE",
  IDLE_GPU_HOG: "CONTAINER_PAUSE",
  HOST_MEMORY_LEAK: "SIGSTOP_FREEZE",
};

function alertToInv(alert: ConsolidatedAlert): ActiveInterventionNode | null {
  const action = ALERT_TO_ACTION[alert.anomalyType];
  if (!action) return null;
  return {
    mitigationId: alert.eventId,
    nodeId: alert.nodeId || "unknown",
    targetPid: alert.targetPid,
    processName: `PID ${alert.targetPid}`,
    appliedAction: action,
    executionTimestamp: alert.timestamp,
    currentStatus: "ACTIVE_ENFORCED",
    policyViolationReason: alert.anomalyType.replace(/_/g, " "),
  };
}

function parseAlert(raw: unknown): ConsolidatedAlert | null {
  if (!raw || typeof raw !== "object") return null;
  const obj = raw as Record<string, unknown>;

  const eventId = getStr(obj, "eventId", "event_id");
  const timestamp = getNum(obj, "timestamp");
  if (!eventId || !timestamp) return null;

  const payload = (obj.alert_payload || obj.alertPayload || obj) as Record<string, unknown>;

  const targetPid = getNum(payload, "targetPid", "target_pid");
  if (!targetPid) return null;

  const telemetryRaw = (payload.telemetry_snapshot || payload.telemetry) as Record<string, unknown> | undefined;

  return {
    eventId,
    timestamp,
    targetPid,
    gpuUuid: getStr(payload, "gpuUuid", "gpu_uuid"),
    anomalyType: getStr(payload, "anomalyType", "anomaly_type", "anomaly_type"),
    severity: (getStr(payload, "severity").toUpperCase() === "CRITICAL" ? "CRITICAL" : "WARNING") as ConsolidatedAlert["severity"],
    nodeId: getStr(obj, "nodeId", "node_id", "agentId", "agent_id", "localNodeId", "local_node_id"),
    tenantId: getStr(obj, "tenantId", "tenant_id"),
    telemetry: telemetryRaw
      ? {
          smUtilizationPct: getNum(telemetryRaw, "smUtilizationPct", "sm_utilization_pct"),
          vramUsedBytes: getNum(telemetryRaw, "vramUsedBytes", "vram_used_bytes"),
        }
      : undefined,
    metadata: undefined,
  };
}

export function App() {
  const [activeTab, setActiveTab] = useState<TabId>("dashboard");
  const upsertProxy = useMetricsStore((s) => s.upsertProxy);
  const pushAlert = useMetricsStore((s) => s.pushAlert);
  const pushIntervention = useMitigationStore((s) => s.pushIntervention);
  const pushToast = useMitigationStore((s) => s.pushToast);

  const [logLines, setLogLines] = useState<RealTimeLogLine[]>([]);
  const syntheticSourceRef = useRef<ReturnType<typeof createSyntheticLogSource> | null>(null);

  useEffect(() => {
    if (activeTab !== "logs") {
      syntheticSourceRef.current?.stop();
      return;
    }
    const source = createSyntheticLogSource((line) => {
      setLogLines((prev) => [...prev, line]);
    });
    syntheticSourceRef.current = source;
    source.start();
    return () => source.stop();
  }, [activeTab]);

  const pushLogLineRef = useRef<(line: RealTimeLogLine) => void>(() => {});
  useEffect(() => {
    pushLogLineRef.current = (line: RealTimeLogLine) => {
      setLogLines((prev) => [...prev, line]);
    };
  }, []);

  function handleSSEMessage(raw: unknown) {
      const alert = parseAlert(raw);
      if (!alert) return;

      upsertProxy(
        alert.nodeId || "unknown",
        alert.tenantId || "default",
        {
          pid: alert.targetPid,
          processName: `PID ${alert.targetPid}`,
          targetModel: "",
          cumulativeTokens: 0,
          anomalyStatus: normalizeAnomalyType(alert.anomalyType),
          containerId: undefined,
        },
        0,
        0,
        alert.telemetry
          ? [
              {
                uuid: alert.gpuUuid,
                modelName: "",
                smUtilizationPct: alert.telemetry.smUtilizationPct,
                vramUsedBytes: alert.telemetry.vramUsedBytes,
                memoryTotalBytes: 0,
                temperatureCelsius: 0,
                powerDrawWatts: 0,
                graphicsClockMHz: 0,
                memoryClockMHz: 0,
              },
            ]
          : [],
        alert.timestamp
      );

      pushAlert(alert);

      pushLogLineRef.current({
        id: alert.eventId,
        timestamp: alert.timestamp,
        originNodeId: alert.nodeId || "unknown",
        logLevel: alert.severity === "CRITICAL" ? "CRITICAL" : "WARN",
        messagePayload: `\x1b[1;31m${alert.anomalyType}\x1b[0m on PID ${alert.targetPid}`,
      });

      const inv = alertToInv(alert);
      if (inv) {
        pushIntervention(inv);
        pushToast({
          message: `New intervention: ${inv.appliedAction} on PID ${inv.targetPid}`,
          type: "warning",
        });
      }
    }

  const { isConnected, reconnectAttempt } = useSystemStream({
    url: SSE_URL,
    token: AUTH_TOKEN,
    onAlert: handleSSEMessage,
    enabled: true,
  });

  const tabs: { id: TabId; label: string }[] = [
    { id: "dashboard", label: "Dashboard" },
    { id: "mitigation", label: "Mitigation Console" },
    { id: "logs", label: "Terminal Logs" },
  ];

  return (
    <div className="h-full flex flex-col">
      <header className="flex items-center justify-between px-4 py-2 bg-zinc-900 border-b border-zinc-800 shrink-0">
        <div className="flex items-center gap-4">
          <span className="text-sm font-bold tracking-wider uppercase text-zinc-200">
            AI Compute Profiler
          </span>
          <nav className="flex items-center gap-1 ml-2">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`px-3 py-1 rounded text-[12px] font-medium transition-all ${
                  activeTab === tab.id
                    ? "bg-zinc-700/60 text-zinc-100 shadow-sm"
                    : "text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50"
                }`}
              >
                {tab.label}
              </button>
            ))}
          </nav>
          <span
            className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-mono font-semibold ${
              isConnected
                ? "bg-emerald-900/50 text-emerald-300"
                : "bg-rose-900/50 text-rose-300"
            }`}
          >
            <span
              className={`inline-block w-1.5 h-1.5 rounded-full ${
                isConnected ? "bg-emerald-400" : "bg-rose-400"
              } ${isConnected ? "" : "animate-pulse"}`}
            />
            {isConnected ? "LIVE" : `RECONNECT ${reconnectAttempt}`}
          </span>
        </div>
        <div className="flex items-center gap-3 text-[11px] text-zinc-600">
          <span>{userProfile.role}</span>
          <span className="text-zinc-700">|</span>
          <span>GET /api/v2/events/stream</span>
        </div>
      </header>

      {activeTab === "dashboard" ? (
        <MetricsDashboard />
      ) : activeTab === "mitigation" ? (
        <MitigationConsole userProfile={userProfile} />
      ) : (
        <SystemLogTerminal
          tenantId={userProfile.userId}
          activeTargetNodeId="all"
          incomingLogStream={logLines}
          maxBufferLinesCount={2500}
        />
      )}
    </div>
  );
}
