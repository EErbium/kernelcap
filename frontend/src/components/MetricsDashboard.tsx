import { useMemo, useCallback } from "react";
import { shallow } from "zustand/shallow";
import { MetricsGrid } from "./MetricsGrid";

import { SystemMetricsChart } from "./SystemMetricsChart";
import { EfficiencyScatterPlot } from "./EfficiencyScatterPlot";
import { useSlidingTimeBuffer } from "../hooks/useSlidingTimeBuffer";
import { useTelemetryPoller } from "../hooks/useTelemetryPoller";
import { useMetricsStore } from "../hooks/useMetricsStore";
import { CrosshairProvider } from "../hooks/useSynchronizedCrosshair";
import { DEFAULT_TIME_WINDOW_SECONDS } from "../types/charting";
import type { ScatterPoint, ChartTimelinePayload } from "../types/charting";

const POLL_ENDPOINT = "/api/v1/metrics";
const AUTH_TOKEN = import.meta.env.VITE_API_TOKEN ?? "";
const USE_SYNTHETIC = true;

function payloadToScatterPoint(
  p: ChartTimelinePayload,
  label: string,
  status: string
): ScatterPoint {
  return {
    timestamp: p.timestamp,
    smUtilizationPct: p.seriesData.gpuSmUtilization,
    vramAllocatedMb: p.seriesData.gpuVramAllocatedMb,
    anomalyStatus: p.activeAnomalies.length > 0 ? p.activeAnomalies[0].type : status,
    label,
  };
}

export function MetricsDashboard() {
  const buffer = useSlidingTimeBuffer(DEFAULT_TIME_WINDOW_SECONDS);

  const { selectedPid, selectedRowData } = useMetricsStore(
    (s) => ({
      selectedPid: s.selectedPid,
      selectedRowData:
        s.selectedPid !== null
          ? Array.from(s.rows.values()).find(
              (r) => r.proxy.pid === s.selectedPid
            ) ?? null
          : null,
    }),
    shallow
  );

  const handlePayload = useCallback(
    (payload: ChartTimelinePayload) => {
      buffer.push(payload);
    },
    [buffer]
  );

  useTelemetryPoller({
    endpoint: POLL_ENDPOINT,
    token: AUTH_TOKEN,
    enabled: true,
    useSyntheticFallback: USE_SYNTHETIC,
    onPayload: handlePayload,
  });

  const snapshot = buffer.getSnapshot();

  const scatterPoints: ScatterPoint[] = useMemo(() => {
    const base = snapshot.map((p) =>
      payloadToScatterPoint(p, "system", "HEALTHY")
    );

    if (selectedRowData) {
      const gpu = selectedRowData.metrics.activeGpus[0];
      if (gpu && snapshot.length > 0) {
        base.push({
          timestamp: Date.now() / 1000,
          smUtilizationPct: gpu.smUtilizationPct,
          vramAllocatedMb: gpu.vramUsedBytes / (1024 * 1024),
          anomalyStatus: selectedRowData.proxy.anomalyStatus,
          label: `PID ${selectedRowData.proxy.pid} - ${selectedRowData.proxy.processName}`,
        });
      }
    }

    return base;
  }, [snapshot, selectedRowData]);

  return (
    <CrosshairProvider>
      <div className="flex flex-col h-full">
        <div className="flex-1 min-h-0 border-b border-zinc-800/60">
          <MetricsGrid />
        </div>

        <div className="h-[1px] bg-zinc-800/40 shrink-0" />

        <div className="shrink-0 border-t border-zinc-800/40 bg-zinc-900/50">
          <div className="flex items-center justify-between px-4 py-1.5">
            <h3 className="text-[11px] font-semibold tracking-wider uppercase text-zinc-400">
              Time-Series Analytics
            </h3>
            <div className="flex items-center gap-4 text-[10px] text-zinc-600">
              <span className="flex items-center gap-1.5">
                <span className="inline-block w-2.5 h-0.5 rounded bg-cyan-400" />
                SM Utilization
              </span>
              <span className="flex items-center gap-1.5">
                <span className="inline-block w-2.5 h-0.5 rounded bg-violet-400" />
                VRAM Allocated
              </span>
              <span className="flex items-center gap-1.5">
                <span className="inline-block w-2.5 h-0.5 rounded bg-orange-400" />
                CPU
              </span>
              <span className="flex items-center gap-1.5">
                <span className="inline-block w-2.5 h-0.5 rounded bg-green-400" />
                Memory
              </span>
              <span className="text-zinc-700">|</span>
              <span>
                {snapshot.length > 0
                  ? `${snapshot.length} pts (${Math.min(
                      DEFAULT_TIME_WINDOW_SECONDS,
                      Math.round(
                        (snapshot[snapshot.length - 1].timestamp -
                          snapshot[0].timestamp)
                      )
                    )}s window)`
                  : "No data"}
              </span>
            </div>
          </div>

          <div className="flex gap-1 px-1 pb-1">
            <div className="flex-1 min-w-0 rounded border border-zinc-800/50 overflow-hidden">
              <div className="flex items-center justify-between px-3 py-1 bg-zinc-900/80 border-b border-zinc-800/40">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                  GPU & System Metrics
                </span>
                <span className="text-[10px] text-zinc-600">
                  {selectedPid
                    ? `PID ${selectedPid} selected`
                    : "system-wide"}
                </span>
              </div>
              <SystemMetricsChart
                buffer={buffer}
                version={buffer.version}
                height={240}
              />
            </div>

            <div className="flex-1 min-w-0 rounded border border-zinc-800/50 overflow-hidden">
              <div className="flex items-center justify-between px-3 py-1 bg-zinc-900/80 border-b border-zinc-800/40">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                  SM vs VRAM Efficiency
                </span>
                <span className="text-[10px] text-zinc-600">
                  {scatterPoints.length} points
                </span>
              </div>
              <EfficiencyScatterPlot
                points={scatterPoints}
                height={240}
              />
            </div>
          </div>
        </div>
      </div>
    </CrosshairProvider>
  );
}
