import { create } from "zustand";
import type {
  MonitoredNodeState,
  ProxyProcessMetrics,
  GPUDeviceMetrics,
  ConsolidatedAlert,
  CompoundKey,
} from "../types/metrics";
import { createCompoundKey } from "../types/metrics";

export interface MetricsStoreState {
  rows: Map<CompoundKey, MonitoredNodeState & { proxy: ProxyProcessMetrics }>;
  alerts: ConsolidatedAlert[];
  nodeOrder: CompoundKey[];
  selectedPid: number | null;

  upsertProxy: (
    nodeId: string,
    tenantId: string,
    proxy: ProxyProcessMetrics,
    cpuUtilizationPct: number,
    memoryUsedBytes: number,
    activeGpus: GPUDeviceMetrics[],
    timestamp: number
  ) => void;

  removeProxy: (nodeId: string, tenantId: string, pid: number) => void;

  pushAlert: (alert: ConsolidatedAlert) => void;

  clearAlerts: () => void;

  setSelectedPid: (pid: number | null) => void;

  getRowsSnapshot: () => Array<MonitoredNodeState & { proxy: ProxyProcessMetrics }>;
}



function upsertOrder(
  order: CompoundKey[],
  key: CompoundKey,
  anomalyStatus: string
): CompoundKey[] {
  const existingIdx = order.indexOf(key);
  if (anomalyStatus !== "HEALTHY") {
    if (existingIdx === 0) return order;
    const next = order.filter((k) => k !== key);
    next.unshift(key);
    return next;
  }
  if (existingIdx === -1) {
    return [...order, key];
  }
  return order;
}

export const useMetricsStore = create<MetricsStoreState>((set, get) => ({
  rows: new Map(),
  alerts: [],
  nodeOrder: [],
  selectedPid: null,

  upsertProxy: (
    nodeId,
    tenantId,
    proxy,
    cpuUtilizationPct,
    memoryUsedBytes,
    activeGpus,
    timestamp
  ) => {
    const key = createCompoundKey(tenantId, nodeId, proxy.pid);
    set((state) => {
      const next = new Map(state.rows);
      next.set(key, {
        nodeId,
        tenantId,
        lastSeenTimestamp: timestamp,
        proxy,
        metrics: {
          cpuUtilizationPct,
          memoryUsedBytes,
          activeGpus: activeGpus ?? [],
          activeProxies: [],
        },
      });
      return {
        rows: next,
        nodeOrder: upsertOrder(state.nodeOrder, key, proxy.anomalyStatus),
      };
    });
  },

  removeProxy: (nodeId, tenantId, pid) => {
    const key = createCompoundKey(tenantId, nodeId, pid);
    set((state) => {
      const next = new Map(state.rows);
      next.delete(key);
      return {
        rows: next,
        nodeOrder: state.nodeOrder.filter((k) => k !== key),
      };
    });
  },

  pushAlert: (alert) => {
    set((state) => ({
      alerts: [alert, ...state.alerts].slice(0, 500),
    }));
  },

  clearAlerts: () => set({ alerts: [] }),

  setSelectedPid: (pid) => set({ selectedPid: pid }),

  getRowsSnapshot: () => {
    const state = get();
    return state.nodeOrder
      .map((key) => state.rows.get(key))
      .filter(Boolean) as Array<MonitoredNodeState & { proxy: ProxyProcessMetrics }>;
  },
}));
