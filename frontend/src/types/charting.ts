export interface ChartTimelinePayload {
  timestamp: number;
  seriesData: {
    cpuUtilization: number;
    memoryUsageMb: number;
    gpuSmUtilization: number;
    gpuVramAllocatedMb: number;
    tokenIngestionRate: number;
  };
  activeAnomalies: Array<{
    id: string;
    type: "SEMANTIC_LOOP" | "IDLE_HOG" | "MEMORY_LEAK";
    severity: "WARNING" | "CRITICAL";
  }>;
}

export interface ScatterPoint {
  timestamp: number;
  smUtilizationPct: number;
  vramAllocatedMb: number;
  anomalyStatus: string;
  label: string;
}

export interface MetricChartProps {
  tenantId: string;
  nodeId: string;
  targetPid: number;
  timeWindowSeconds: number;
  streamData: ChartTimelinePayload[];
}

export const DEFAULT_TIME_WINDOW_SECONDS = 300;
export const CHART_POLL_INTERVAL_MS = 2_000;
export const MAX_CACHED_POINTS = 5_000;


export function createEmptyTimelinePayload(
  timestamp: number
): ChartTimelinePayload {
  return {
    timestamp,
    seriesData: {
      cpuUtilization: 0,
      memoryUsageMb: 0,
      gpuSmUtilization: 0,
      gpuVramAllocatedMb: 0,
      tokenIngestionRate: 0,
    },
    activeAnomalies: [],
  };
}
