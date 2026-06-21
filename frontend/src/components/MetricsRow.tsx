import { memo, useMemo } from "react";
import type { MonitoredNodeState, ProxyProcessMetrics } from "../types/metrics";
import { AlertBadge } from "./AlertBadge";



export type RowData = MonitoredNodeState & { proxy: ProxyProcessMetrics };

interface MetricsRowProps {
  data: RowData;
  style?: React.CSSProperties;
  isSelected?: boolean;
  onSelect?: (pid: number) => void;
}

function formatBytes(bytes: number): string {
  if (bytes >= 1_073_741_824) return `${(bytes / 1_073_741_824).toFixed(1)}GiB`;
  if (bytes >= 1_048_576) return `${(bytes / 1_048_576).toFixed(1)}MiB`;
  if (bytes >= 1_024) return `${(bytes / 1_024).toFixed(1)}KiB`;
  return `${bytes}B`;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

const anomalyBorder: Record<string, string> = {
  SEMANTIC_LOOP:
    "border-l-4 border-l-amber-500 bg-amber-500/5 animate-alert-strobe-amber",
  IDLE_HOG:
    "border-l-4 border-l-rose-500 bg-rose-500/5 animate-alert-strobe-rose",
};

export const MetricsRow = memo(function MetricsRow({
  data,
  style,
  isSelected,
  onSelect,
}: MetricsRowProps) {
  const { nodeId, proxy, metrics, tenantId } = data;
  const gpu = metrics.activeGpus[0];
  const anomalyClass = useMemo(
    () =>
      proxy.anomalyStatus !== "HEALTHY"
        ? anomalyBorder[proxy.anomalyStatus] ?? ""
        : "",
    [proxy.anomalyStatus]
  );

  return (
    <div
      style={style}
      onClick={() => onSelect?.(proxy.pid)}
      className={`flex items-center w-full text-[13px] font-mono border-b border-zinc-800/60 hover:bg-zinc-800/30 transition-colors duration-150 cursor-pointer ${
        isSelected ? "bg-zinc-800/40 ring-1 ring-inset ring-cyan-700/40" : ""
      } ${anomalyClass}`}
    >
      <div className="flex items-center gap-2 w-[180px] shrink-0 px-3 py-2 truncate">
        <span className="text-zinc-400 text-[11px] w-8 truncate">{tenantId}</span>
        <span className="text-zinc-100 font-medium truncate">{proxy.processName || `PID ${proxy.pid}`}</span>
      </div>

      <div className="w-[70px] shrink-0 px-2 text-right tabular-nums">
        <span
          className={`${
            metrics.cpuUtilizationPct > 80
              ? "text-rose-400"
              : metrics.cpuUtilizationPct > 50
              ? "text-amber-400"
              : "text-zinc-300"
          }`}
        >
          {metrics.cpuUtilizationPct.toFixed(1)}%
        </span>
      </div>

      <div className="w-[90px] shrink-0 px-2 text-right tabular-nums text-zinc-300">
        {formatBytes(metrics.memoryUsedBytes)}
      </div>

      <div className="w-[70px] shrink-0 px-2 text-right tabular-nums">
        {gpu ? (
          <span
            className={`${
              gpu.smUtilizationPct < 20 && proxy.anomalyStatus === "IDLE_HOG"
                ? "text-rose-400"
                : gpu.smUtilizationPct > 80
                ? "text-emerald-400"
                : "text-zinc-300"
            }`}
          >
            {gpu.smUtilizationPct.toFixed(1)}%
          </span>
        ) : (
          <span className="text-zinc-600">&mdash;</span>
        )}
      </div>

      <div className="w-[90px] shrink-0 px-2 text-right tabular-nums text-zinc-300">
        {gpu ? formatBytes(gpu.vramUsedBytes) : <span className="text-zinc-600">&mdash;</span>}
      </div>

      <div className="w-[80px] shrink-0 px-2 text-right tabular-nums text-zinc-300">
        {proxy.targetModel ? (
          <span className="text-[11px] truncate block" title={proxy.targetModel}>
            {proxy.targetModel}
          </span>
        ) : (
          <span className="text-zinc-600">&mdash;</span>
        )}
      </div>

      <div className="w-[80px] shrink-0 px-2 text-right tabular-nums text-zinc-300">
        {formatTokens(proxy.cumulativeTokens)}
      </div>

      <div className="w-[80px] shrink-0 px-2 text-right">
        <AlertBadge status={proxy.anomalyStatus} />
      </div>

      <div className="flex-1 px-3 text-[11px] text-zinc-600 truncate" title={nodeId}>
        {nodeId}
      </div>
    </div>
  );
});
