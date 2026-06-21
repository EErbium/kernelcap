import { useRef, useCallback, useState } from "react";
import { DEFAULT_TIME_WINDOW_SECONDS, MAX_CACHED_POINTS } from "../types/charting";
export function useSlidingTimeBuffer(timeWindowSeconds = DEFAULT_TIME_WINDOW_SECONDS) {
    const bufferRef = useRef([]);
    const [version, setVersion] = useState(0);
    const rafRef = useRef(null);
    const pendingRef = useRef([]);
    const flush = useCallback(() => {
        rafRef.current = null;
        const incoming = pendingRef.current;
        pendingRef.current = [];
        if (incoming.length === 0)
            return;
        const buf = bufferRef.current;
        const now = incoming[incoming.length - 1]?.timestamp ?? Date.now() / 1000;
        const cutoff = now - timeWindowSeconds;
        for (const p of incoming) {
            if (p.timestamp < cutoff)
                continue;
            const dup = buf.length > 0 && buf[buf.length - 1].timestamp === p.timestamp;
            if (dup) {
                buf[buf.length - 1] = p;
            }
            else {
                buf.push(p);
            }
        }
        while (buf.length > 0 && buf[0].timestamp < cutoff) {
            buf.shift();
        }
        if (buf.length > MAX_CACHED_POINTS) {
            buf.splice(0, buf.length - MAX_CACHED_POINTS);
        }
        setVersion((v) => v + 1);
    }, [timeWindowSeconds]);
    const scheduleFlush = useCallback(() => {
        if (rafRef.current === null) {
            rafRef.current = requestAnimationFrame(flush);
        }
    }, [flush]);
    const push = useCallback((point) => {
        pendingRef.current.push(point);
        scheduleFlush();
    }, [scheduleFlush]);
    const pushBatch = useCallback((points) => {
        for (const p of points) {
            pendingRef.current.push(p);
        }
        scheduleFlush();
    }, [scheduleFlush]);
    function getSnapshot() { return bufferRef.current; }
    function clear() {
        bufferRef.current = [];
        pendingRef.current = [];
        setVersion(0);
    }
    return {
        push,
        pushBatch,
        getSnapshot,
        clear,
        version,
        count: bufferRef.current.length,
    };
}
