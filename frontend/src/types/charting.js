export const DEFAULT_TIME_WINDOW_SECONDS = 300;
export const CHART_POLL_INTERVAL_MS = 2_000;
export const MAX_CACHED_POINTS = 5_000;
export function createEmptyTimelinePayload(timestamp) {
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
