import { useRef, useCallback, useState } from "react";
import { DEFAULT_MAX_BUFFER_LINES } from "../types/logging";
export function useLogBuffer(maxLines = DEFAULT_MAX_BUFFER_LINES) {
    const bufferRef = useRef([]);
    const [version, setVersion] = useState(0);
    const pushLines = useCallback((lines) => {
        if (lines.length === 0)
            return;
        const buf = bufferRef.current;
        for (const line of lines) {
            if (buf.length >= maxLines) {
                buf.shift();
            }
            buf.push(line);
        }
        setVersion((v) => v + 1);
    }, [maxLines]);
    const pushLine = useCallback((line) => {
        pushLines([line]);
    }, [pushLines]);
    function getSnapshot() { return bufferRef.current; }
    function clear() {
        bufferRef.current = [];
        setVersion(0);
    }
    return {
        pushLines,
        pushLine,
        getSnapshot,
        clear,
        version,
        count: bufferRef.current.length,
    };
}
