let counter = BigInt(Date.now());
function uid() {
    counter += 1n;
    return `syn-${counter.toString(36)}`;
}
let lastSm = 50;
let lastVram = 2048;
let lastCpu = 40;
let lastMem = 8192;
let lastTokens = 100;
function clampedWalk(current, step, min, max) {
    const next = current + (Math.random() - 0.5) * step * 2;
    return Math.min(max, Math.max(min, next));
}
export function generateSyntheticPayload(baseTimestamp) {
    const timestamp = baseTimestamp ?? Math.floor(Date.now() / 1000);
    lastSm = clampedWalk(lastSm, 8, 0, 100);
    lastVram = clampedWalk(lastVram, 256, 0, 24000);
    lastCpu = clampedWalk(lastCpu, 6, 0, 100);
    lastMem = clampedWalk(lastMem, 512, 1024, 64000);
    lastTokens = clampedWalk(lastTokens, 30, 0, 2000);
    const hasAnomaly = Math.random() < 0.05;
    const anomalyTypes = ["SEMANTIC_LOOP", "IDLE_HOG", "MEMORY_LEAK"];
    return {
        timestamp,
        seriesData: {
            cpuUtilization: Math.round(lastCpu * 10) / 10,
            memoryUsageMb: Math.round(lastMem),
            gpuSmUtilization: Math.round(lastSm * 10) / 10,
            gpuVramAllocatedMb: Math.round(lastVram),
            tokenIngestionRate: Math.round(lastTokens),
        },
        activeAnomalies: hasAnomaly
            ? [
                {
                    id: uid(),
                    type: anomalyTypes[Math.floor(Math.random() * anomalyTypes.length)],
                    severity: Math.random() < 0.3 ? "CRITICAL" : "WARNING",
                },
            ]
            : [],
    };
}
export function generateBackfillPayloads(count, intervalSeconds = 2) {
    const now = Math.floor(Date.now() / 1000);
    const result = [];
    for (let i = count - 1; i >= 0; i--) {
        result.push(generateSyntheticPayload(now - i * intervalSeconds));
    }
    return result;
}
export function resetSyntheticSeed() {
    lastSm = 50;
    lastVram = 2048;
    lastCpu = 40;
    lastMem = 8192;
    lastTokens = 100;
}
