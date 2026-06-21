import { jsx as _jsx } from "react/jsx-runtime";
import { createContext, useContext, useRef, useCallback, useEffect } from "react";
const CrosshairContext = createContext(null);
export function CrosshairProvider({ children }) {
    const tsRef = useRef(null);
    const listenersRef = useRef(new Set());
    const setHoveredTimestamp = useCallback((ts) => {
        tsRef.current = ts;
        listenersRef.current.forEach((fn) => fn());
    }, []);
    const subscribe = useCallback((listener) => {
        listenersRef.current.add(listener);
        return () => {
            listenersRef.current.delete(listener);
        };
    }, []);
    return (_jsx(CrosshairContext.Provider, { value: {
            hoveredTimestamp: null,
            setHoveredTimestamp,
            subscribe,
        }, children: children }));
}
const _ctxErr = "useSynchronizedCrosshair must be used within a <CrosshairProvider>";
export function useSynchronizedCrosshair() {
    const ctx = useContext(CrosshairContext);
    if (!ctx) {
        throw new Error(_ctxErr);
    }
    return ctx;
}
export function useCrosshairSubscription(onCrosshair) {
    const { subscribe } = useSynchronizedCrosshair();
    useEffect(() => {
        return subscribe(() => {
            onCrosshair(getGlobalCrosshair());
        });
    }, [subscribe, onCrosshair]);
}
const crosshairSingletonRef = { current: null };
const crosshairListeners = new Set();
export function setGlobalCrosshair(ts) {
    crosshairSingletonRef.current = ts;
    crosshairListeners.forEach((fn) => fn());
}
export function getGlobalCrosshair() {
    return crosshairSingletonRef.current;
}
export function subscribeGlobalCrosshair(fn) {
    crosshairListeners.add(fn);
    return () => crosshairListeners.delete(fn);
}
