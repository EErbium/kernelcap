import { useEffect, useRef, useCallback, useState } from "react";
import { calculateBackoff } from "../utils/sseBackoff";
const AUTH_QUERY_PARAM = "auth_token";
function buildStreamUrl(baseUrl, token) {
    const separator = baseUrl.includes("?") ? "&" : "?";
    return `${baseUrl}${separator}${AUTH_QUERY_PARAM}=${encodeURIComponent(token)}`;
}
export function useSystemStream(options) {
    const { url, token, onAlert, onConnected, onError, enabled = true } = options;
    const eventSourceRef = useRef(null);
    const reconnectTimerRef = useRef(null);
    const unmountedRef = useRef(false);
    const attemptRef = useRef(0);
    const [reconnectAttempt, setReconnectAttempt] = useState(0);
    const [isConnected, setIsConnected] = useState(false);
    const onAlertRef = useRef(onAlert);
    onAlertRef.current = onAlert;
    const onConnectedRef = useRef(onConnected);
    onConnectedRef.current = onConnected;
    const onErrorRef = useRef(onError);
    onErrorRef.current = onError;
    const connect = useCallback(() => {
        if (unmountedRef.current || !enabled)
            return;
        if (eventSourceRef.current) {
            eventSourceRef.current.close();
        }
        const streamUrl = buildStreamUrl(url, token);
        const es = new EventSource(streamUrl);
        eventSourceRef.current = es;
        es.onopen = () => {
            if (unmountedRef.current) {
                es.close();
                return;
            }
            attemptRef.current = 0;
            setIsConnected(true);
            onConnectedRef.current?.();
        };
        es.onmessage = (event) => {
            if (unmountedRef.current)
                return;
            try {
                const parsed = JSON.parse(event.data);
                onAlertRef.current?.(parsed);
            }
            catch {
            }
        };
        es.addEventListener("alert", (event) => {
            if (unmountedRef.current)
                return;
            try {
                const parsed = JSON.parse(event.data);
                onAlertRef.current?.(parsed);
            }
            catch {
            }
        });
        es.onerror = (err) => {
            if (unmountedRef.current)
                return;
            setIsConnected(false);
            onErrorRef.current?.(err);
            es.close();
            eventSourceRef.current = null;
            scheduleReconnect();
        };
    }, [url, token, enabled]);
    const connectRef = useRef(connect);
    connectRef.current = connect;
    const scheduleReconnect = useCallback(() => {
        if (unmountedRef.current)
            return;
        const delay = calculateBackoff(attemptRef.current);
        attemptRef.current += 1;
        setReconnectAttempt(attemptRef.current);
        reconnectTimerRef.current = setTimeout(() => {
            if (!unmountedRef.current) {
                connectRef.current();
            }
        }, delay);
    }, []);
    useEffect(() => {
        unmountedRef.current = false;
        if (enabled) {
            connect();
        }
        return () => {
            unmountedRef.current = true;
            if (reconnectTimerRef.current) {
                clearTimeout(reconnectTimerRef.current);
                reconnectTimerRef.current = null;
            }
            if (eventSourceRef.current) {
                eventSourceRef.current.close();
                eventSourceRef.current = null;
            }
            setIsConnected(false);
        };
    }, [enabled, connect]);
    return { isConnected, reconnectAttempt };
}
