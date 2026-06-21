import { create } from "zustand";
import { createCompoundKey } from "../types/metrics";
function upsertOrder(order, key, anomalyStatus) {
    const existingIdx = order.indexOf(key);
    if (anomalyStatus !== "HEALTHY") {
        if (existingIdx === 0)
            return order;
        const next = order.filter((k) => k !== key);
        next.unshift(key);
        return next;
    }
    if (existingIdx === -1) {
        return [...order, key];
    }
    return order;
}
export const useMetricsStore = create((set, get) => ({
    rows: new Map(),
    alerts: [],
    nodeOrder: [],
    selectedPid: null,
    upsertProxy: (nodeId, tenantId, proxy, cpuUtilizationPct, memoryUsedBytes, activeGpus, timestamp) => {
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
            .filter(Boolean);
    },
}));
