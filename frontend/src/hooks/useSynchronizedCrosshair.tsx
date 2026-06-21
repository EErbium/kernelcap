import { createContext, useContext, useRef, useCallback, useEffect } from "react";

export interface CrosshairState {
  hoveredTimestamp: number | null;
  setHoveredTimestamp: (ts: number | null) => void;
  subscribe: (listener: () => void) => () => void;
}

const CrosshairContext = createContext<CrosshairState | null>(null);

export function CrosshairProvider({ children }: { children: React.ReactNode }) {
  const tsRef = useRef<number | null>(null);
  const listenersRef = useRef<Set<() => void>>(new Set());

  const setHoveredTimestamp = useCallback((ts: number | null) => {
    tsRef.current = ts;
    listenersRef.current.forEach((fn) => fn());
  }, []);

  const subscribe = useCallback((listener: () => void) => {
    listenersRef.current.add(listener);
    return () => {
      listenersRef.current.delete(listener);
    };
  }, []);

  return (
    <CrosshairContext.Provider
      value={{
        hoveredTimestamp: null,
        setHoveredTimestamp,
        subscribe,
      }}
    >
      {children}
    </CrosshairContext.Provider>
  );
}

const _ctxErr = "useSynchronizedCrosshair must be used within a <CrosshairProvider>";

export function useSynchronizedCrosshair(): CrosshairState {
  const ctx = useContext(CrosshairContext);
  if (!ctx) {
    throw new Error(_ctxErr);
  }
  return ctx;
}

export function useCrosshairSubscription(
  onCrosshair: (ts: number | null) => void
) {
  const { subscribe } = useSynchronizedCrosshair();

  useEffect(() => {
    return subscribe(() => {
      onCrosshair(getGlobalCrosshair());
    });
  }, [subscribe, onCrosshair]);
}

const crosshairSingletonRef = { current: null as number | null };
const crosshairListeners = new Set<() => void>();

export function setGlobalCrosshair(ts: number | null) {
  crosshairSingletonRef.current = ts;
  crosshairListeners.forEach((fn) => fn());
}

export function getGlobalCrosshair(): number | null {
  return crosshairSingletonRef.current;
}

export function subscribeGlobalCrosshair(fn: () => void): () => void {
  crosshairListeners.add(fn);
  return () => crosshairListeners.delete(fn);
}
