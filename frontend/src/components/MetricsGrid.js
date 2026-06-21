import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useRef, useState, useCallback, useEffect, useMemo } from "react";
import { shallow } from "zustand/shallow";
import { useMetricsStore } from "../hooks/useMetricsStore";
import { MetricsRow } from "./MetricsRow";
const ROW_HEIGHT = 40;
const OVERSCAN = 10;
export function MetricsGrid() {
    const containerRef = useRef(null);
    const [scrollTop, setScrollTop] = useState(0);
    const [containerHeight, setContainerHeight] = useState(0);
    const { nodeOrder, rowMap, alertsCount, selectedPid } = useMetricsStore((s) => ({
        nodeOrder: s.nodeOrder,
        rowMap: s.rows,
        alertsCount: s.alerts.length,
        selectedPid: s.selectedPid,
    }), shallow);
    const alerts = useMetricsStore((s) => s.alerts);
    const clearAlerts = useMetricsStore((s) => s.clearAlerts);
    const setSelectedPid = useMetricsStore((s) => s.setSelectedPid);
    const data = useMemo(() => nodeOrder
        .map((key) => rowMap.get(key))
        .filter(Boolean), [nodeOrder, rowMap]);
    useEffect(() => {
        const el = containerRef.current;
        if (!el)
            return;
        const ro = new ResizeObserver(([entry]) => {
            setContainerHeight(entry.contentRect.height);
        });
        ro.observe(el);
        return () => ro.disconnect();
    }, []);
    const handleScroll = useCallback(() => {
        if (containerRef.current) {
            setScrollTop(containerRef.current.scrollTop);
        }
    }, []);
    const totalHeight = data.length * ROW_HEIGHT;
    const startIdx = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
    const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + OVERSCAN * 2;
    const endIdx = Math.min(data.length, startIdx + visibleCount);
    const visibleRows = useMemo(() => data.slice(startIdx, endIdx), [data, startIdx, endIdx]);
    const latestAlert = alerts[0];
    const handleSelect = useCallback((pid) => {
        setSelectedPid(pid === selectedPid ? null : pid);
    }, [selectedPid, setSelectedPid]);
    return (_jsxs("div", { className: "flex flex-col h-full bg-zinc-950 text-zinc-100", children: [_jsxs("div", { className: "flex items-center justify-between px-4 py-2 border-b border-zinc-800 bg-zinc-900/80", children: [_jsx("h2", { className: "text-sm font-semibold tracking-wide text-zinc-200 uppercase", children: "System Metrics" }), _jsxs("div", { className: "flex items-center gap-3 text-[11px] text-zinc-500", children: [_jsxs("span", { children: [data.length, " processes"] }), _jsx("span", { className: "text-zinc-700", children: "|" }), _jsxs("span", { children: [alertsCount, " alerts"] }), alertsCount > 0 && (_jsx("button", { onClick: clearAlerts, className: "text-zinc-600 hover:text-zinc-300 underline underline-offset-2 transition-colors", children: "clear" }))] })] }), latestAlert && (_jsxs("div", { className: "flex items-center gap-3 px-4 py-1.5 text-[12px] font-mono bg-rose-950/40 border-b border-rose-800/40 text-rose-200 animate-alert-flash", children: [_jsx("span", { className: "inline-block w-2 h-2 rounded-full bg-rose-400 animate-pulse" }), _jsx("span", { className: "font-semibold uppercase text-[11px] tracking-wider", children: latestAlert.severity }), _jsxs("span", { className: "text-zinc-400", children: ["PID ", latestAlert.targetPid] }), _jsx("span", { className: "text-zinc-500", children: latestAlert.anomalyType }), _jsx("span", { className: "ml-auto text-zinc-600 text-[11px]", children: new Date(latestAlert.timestamp * 1000).toLocaleTimeString() })] })), _jsxs("div", { className: "flex items-center h-[36px] text-[11px] font-semibold text-zinc-500 uppercase tracking-wider border-b border-zinc-800/80 bg-zinc-900/50 shrink-0", children: [_jsx("div", { className: "w-[180px] shrink-0 px-3", children: "Process" }), _jsx("div", { className: "w-[70px] shrink-0 px-2 text-right", children: "CPU" }), _jsx("div", { className: "w-[90px] shrink-0 px-2 text-right", children: "RAM" }), _jsx("div", { className: "w-[70px] shrink-0 px-2 text-right", children: "GPU%" }), _jsx("div", { className: "w-[90px] shrink-0 px-2 text-right", children: "VRAM" }), _jsx("div", { className: "w-[80px] shrink-0 px-2 text-right", children: "Model" }), _jsx("div", { className: "w-[80px] shrink-0 px-2 text-right", children: "Tokens" }), _jsx("div", { className: "w-[80px] shrink-0 px-2 text-right", children: "Status" }), _jsx("div", { className: "flex-1 px-3", children: "Node" })] }), _jsx("div", { ref: containerRef, onScroll: handleScroll, className: "flex-1 overflow-y-auto overflow-x-hidden scrollbar-thin scrollbar-track-zinc-900 scrollbar-thumb-zinc-700", children: _jsxs("div", { style: { height: totalHeight, position: "relative" }, children: [visibleRows.map((row, i) => (_jsx(MetricsRow, { data: row, isSelected: row.proxy.pid === selectedPid, onSelect: handleSelect, style: {
                                position: "absolute",
                                top: (startIdx + i) * ROW_HEIGHT,
                                left: 0,
                                right: 0,
                                height: ROW_HEIGHT,
                            } }, `${row.tenantId}-${row.nodeId}-${row.proxy.pid}`))), data.length === 0 && (_jsx("div", { className: "flex items-center justify-center h-full text-zinc-600 text-sm", children: _jsxs("div", { className: "flex flex-col items-center gap-2", children: [_jsx("span", { className: "inline-block w-6 h-6 border-2 border-zinc-700 border-t-zinc-400 rounded-full animate-spin" }), _jsx("span", { children: "Waiting for stream data\u2026" })] }) }))] }) })] }));
}
