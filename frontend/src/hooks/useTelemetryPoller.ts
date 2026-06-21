import { useEffect, useRef } from "react";
import type { ChartTimelinePayload } from "../types/charting";
import { CHART_POLL_INTERVAL_MS } from "../types/charting";
import { generateSyntheticPayload } from "../utils/syntheticData";


export interface TelemetryPollerOptions {
  endpoint: string;
  token: string;
  enabled: boolean;
  useSyntheticFallback: boolean;
  onPayload: (payload: ChartTimelinePayload) => void;
}

async function fetchTelemetry(
  endpoint: string,
  token: string
): Promise<ChartTimelinePayload | null> {
  try {
    const res = await fetch(endpoint, {
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: "application/json",
      },
    });
    if (!res.ok) return null;
    const json = await res.json();

    const timestamp = Math.floor(Date.now() / 1000);

    const cpuUtilization =
      json.cpu_utilization_pct ??
      json.system_performance_self_check?.cpu_load_pct ??
      json.host_metrics?.cpu_utilization_pct ??
      0;

    const memoryTotalBytes =
      json.memory_total_bytes ??
      json.host_metrics?.memory_total_bytes ??
      1;

    const memoryUsedBytes =
      json.memory_used_bytes ??
      json.host_metrics?.memory_used_bytes ??
      0;

    const memoryUsageMb = memoryTotalBytes > 0
      ? Math.round((memoryUsedBytes / memoryTotalBytes) * 100)
      : 0;

    const devices: Array<Record<string, unknown>> =
      json.gpu_devices ?? json.gpuDevices ?? [];

    const avgSm =
      devices.length > 0
        ? devices.reduce(
            (sum: number, d: Record<string, unknown>) =>
              sum + (Number(d.sm_utilization_pct ?? d.smUtilizationPct ?? 0)),
            0
          ) / devices.length
        : 0;

    const totalVramBytes = devices.reduce(
      (sum: number, d: Record<string, unknown>) =>
        sum + (Number(d.memory_used_bytes ?? d.memoryUsedBytes ?? 0)),
      0
    );

    const anomalies: Array<Record<string, unknown>> =
      json.active_system_anomalies ??
      json.activeAnomalies ??
      [];

    return {
      timestamp,
      seriesData: {
        cpuUtilization: Math.round(cpuUtilization * 10) / 10,
        memoryUsageMb,
        gpuSmUtilization: Math.round(avgSm * 10) / 10,
        gpuVramAllocatedMb: Math.round(totalVramBytes / (1024 * 1024)),
        tokenIngestionRate: 0,
      },
      activeAnomalies: anomalies.map(
        (a: Record<string, unknown>) => ({
          id: String(a.id ?? a.event_id ?? ""),
          type: (a.type ?? a.anomaly_type ?? "MEMORY_LEAK") as
            | "SEMANTIC_LOOP"
            | "IDLE_HOG"
            | "MEMORY_LEAK",
          severity: (a.severity ?? "WARNING") as "WARNING" | "CRITICAL",
        })
      ),
    };
  } catch {
    return null;
  }
}



export function useTelemetryPoller(options: TelemetryPollerOptions) {
  const { endpoint, token, enabled, useSyntheticFallback, onPayload } = options;
  const onPayloadRef = useRef(onPayload);
  onPayloadRef.current = onPayload;
  const tickRef = useRef(0);

  useEffect(() => {
    if (!enabled) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | null = null;

    async function tick() {
      if (!active) return;
      tickRef.current += 1;

      let payload: ChartTimelinePayload | null = null;

      if (useSyntheticFallback) {
        payload = generateSyntheticPayload();
      } else {
        payload = await fetchTelemetry(endpoint, token);
      }

      if (payload && active) {
        onPayloadRef.current(payload);
      }

      if (active) {
        timer = setTimeout(tick, CHART_POLL_INTERVAL_MS);
      }
    }

    tick();

    return () => {
      active = false;
      if (timer !== null) clearTimeout(timer);
    };
  }, [endpoint, token, enabled, useSyntheticFallback]);
}
