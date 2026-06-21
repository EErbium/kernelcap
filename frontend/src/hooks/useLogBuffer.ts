import { useRef, useCallback, useState } from "react";
import type { RealTimeLogLine } from "../types/logging";
import { DEFAULT_MAX_BUFFER_LINES } from "../types/logging";



export interface UseLogBufferReturn {
  pushLines: (lines: RealTimeLogLine[]) => void;
  pushLine: (line: RealTimeLogLine) => void;
  getSnapshot: () => RealTimeLogLine[];
  clear: () => void;
  version: number;
  count: number;
}

export function useLogBuffer(
  maxLines: number = DEFAULT_MAX_BUFFER_LINES
): UseLogBufferReturn {
  const bufferRef = useRef<RealTimeLogLine[]>([]);
  const [version, setVersion] = useState(0);

  const pushLines = useCallback(
    (lines: RealTimeLogLine[]) => {
      if (lines.length === 0) return;
      const buf = bufferRef.current;
      for (const line of lines) {
        if (buf.length >= maxLines) {
          buf.shift();
        }
        buf.push(line);
      }
      setVersion((v) => v + 1);
    },
    [maxLines]
  );

  const pushLine = useCallback(
    (line: RealTimeLogLine) => {
      pushLines([line]);
    },
    [pushLines]
  );

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
