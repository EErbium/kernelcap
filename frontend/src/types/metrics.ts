export interface MonitoredNodeState {
  nodeId: string;
  tenantId: string;
  lastSeenTimestamp: number;
  metrics: {
    cpuUtilizationPct: number;
    memoryUsedBytes: number;
    activeGpus: GPUDeviceMetrics[];
    activeProxies: ProxyProcessMetrics[];
  };
}

export interface GPUDeviceMetrics {
  uuid: string;
  modelName: string;
  smUtilizationPct: number;
  vramUsedBytes: number;
  memoryTotalBytes: number;
  temperatureCelsius: number;
  powerDrawWatts: number;
  graphicsClockMHz: number;
  memoryClockMHz: number;
}

export interface ProxyProcessMetrics {
  pid: number;
  processName: string;
  targetModel: string;
  cumulativeTokens: number;
  anomalyStatus: AnomalyStatus;
  containerId?: string;
}

export type AnomalyStatus = "HEALTHY" | "SEMANTIC_LOOP" | "IDLE_HOG";

export type AlertSeverity = "WARNING" | "CRITICAL";

export interface ConsolidatedAlert {
  eventId: string;
  timestamp: number;
  targetPid: number;
  gpuUuid: string;
  anomalyType: string;
  severity: AlertSeverity;
  nodeId: string;
  tenantId: string;
  telemetry?: {
    smUtilizationPct: number;
    vramUsedBytes: number;
  };
  metadata?: {
    isDeduplicated: boolean;
    cumulativeOccurrences: number;
    suppressionWindowSeconds: number;
  };
}

export interface SSEConnectedEvent {
  status: "connected";
}

export interface HostMetrics {
  cpuUtilizationPct: number;
  memoryTotalBytes: number;
  memoryUsedBytes: number;
}

export type SSEEventType = "connected" | "alert" | "heartbeat";

export type CompoundKey = `${string}-${string}-${number}`;



export function createCompoundKey(
  tenantId: string,
  nodeId: string,
  pid: number
): CompoundKey {
  return `${tenantId}-${nodeId}-${pid}`;
}
