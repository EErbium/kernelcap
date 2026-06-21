export const DEFAULT_INITIAL_BACKOFF_MS = 2_000;
export const DEFAULT_MAX_BACKOFF_MS = 30_000;
export const DEFAULT_BACKOFF_MULTIPLIER = 2;
export const DEFAULT_JITTER_FACTOR = 0.2;
export function calculateBackoff(attempt, initialMs = DEFAULT_INITIAL_BACKOFF_MS, maxMs = DEFAULT_MAX_BACKOFF_MS, multiplier = DEFAULT_BACKOFF_MULTIPLIER, jitterFactor = DEFAULT_JITTER_FACTOR) {
    const delay = Math.min(initialMs * Math.pow(multiplier, attempt), maxMs);
    const jitter = delay * jitterFactor * (Math.random() * 2 - 1);
    return Math.round(Math.max(0, delay + jitter));
}
export function resetBackoff() {
    return { attempt: 0, nextDelay: DEFAULT_INITIAL_BACKOFF_MS };
}
export function nextBackoff(state) {
    const nextAttempt = state.attempt + 1;
    return {
        attempt: nextAttempt,
        nextDelay: calculateBackoff(nextAttempt),
    };
}
