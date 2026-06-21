import { jsx as _jsx } from "react/jsx-runtime";
import { useRef, useEffect, useCallback } from "react";
import { setGlobalCrosshair, subscribeGlobalCrosshair, getGlobalCrosshair, } from "../hooks/useSynchronizedCrosshair";
const GRID_COLOR = "rgba(63, 63, 70, 0.4)";
const TEXT_COLOR = "rgba(161, 161, 170, 0.8)";
const CROSSHAIR_COLOR = "rgba(255, 255, 255, 0.35)";
const SM_COLOR = "#22d3ee";
const VRAM_COLOR = "#a78bfa";
const CPU_COLOR = "#fb923c";
const MEM_COLOR = "#4ade80";
const SM_LABEL = "SM Utilization (%)";
const VRAM_LABEL = "VRAM Allocated (MB)";
const CPU_LABEL = "CPU Utilization (%)";
const MEM_LABEL = "Memory Usage (%)";
function drawChart(ctx, state) {
    const { data, dpr, width, height, hoverTs } = state;
    const padding = { top: 24, right: 16, bottom: 32, left: 56 };
    const plotW = width - padding.left - padding.right;
    const plotH = height - padding.top - padding.bottom;
    const ts = hoverTs;
    ctx.save();
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, width, height);
    if (data.length < 2) {
        ctx.fillStyle = TEXT_COLOR;
        ctx.font = "13px 'JetBrains Mono', monospace";
        ctx.textAlign = "center";
        ctx.fillText("Waiting for data...", width / 2, height / 2);
        ctx.restore();
        return;
    }
    const tMin = data[0].timestamp;
    const tMax = data[data.length - 1].timestamp;
    const tRange = Math.max(tMax - tMin, 1);
    const cpuMax = Math.max(...data.map((d) => d.seriesData.cpuUtilization), 1);
    const memMax = Math.max(...data.map((d) => d.seriesData.memoryUsageMb), 1);
    const smMax = 100;
    const vramMax = Math.max(...data.map((d) => d.seriesData.gpuVramAllocatedMb), 1);
    const leftMax = Math.max(cpuMax, memMax, 1);
    const rightMax = Math.max(smMax, vramMax, 1);
    function xPos(ts) {
        return padding.left + ((ts - tMin) / tRange) * plotW;
    }
    function leftY(val) {
        return padding.top + plotH - (val / leftMax) * plotH;
    }
    function rightY(val) {
        return padding.top + plotH - (val / rightMax) * plotH;
    }
    function drawLine(points, yFn, color) {
        if (points.length < 2)
            return;
        ctx.beginPath();
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.5;
        for (let i = 0; i < points.length; i++) {
            const x = xPos(data[i].timestamp);
            const y = yFn === leftY ? leftY(points[i]) : rightY(points[i]);
            if (i === 0)
                ctx.moveTo(x, y);
            else
                ctx.lineTo(x, y);
        }
        ctx.stroke();
    }
    function fillArea(points, yFn, color) {
        if (points.length < 2)
            return;
        const yBase = padding.top + plotH;
        ctx.beginPath();
        ctx.fillStyle = color;
        ctx.moveTo(xPos(data[0].timestamp), yBase);
        for (let i = 0; i < points.length; i++) {
            ctx.lineTo(xPos(data[i].timestamp), yFn(points[i]));
        }
        ctx.lineTo(xPos(data[data.length - 1].timestamp), yBase);
        ctx.closePath();
        ctx.fill();
    }
    const cpuPoints = data.map((d) => d.seriesData.cpuUtilization);
    const memPoints = data.map((d) => d.seriesData.memoryUsageMb);
    const smPoints = data.map((d) => d.seriesData.gpuSmUtilization);
    const vramPoints = data.map((d) => d.seriesData.gpuVramAllocatedMb);
    drawLine(cpuPoints, leftY, CPU_COLOR);
    drawLine(memPoints, leftY, MEM_COLOR);
    fillArea(smPoints, rightY, "rgba(34, 211, 238, 0.08)");
    drawLine(smPoints, rightY, SM_COLOR);
    drawLine(vramPoints, rightY, VRAM_COLOR);
    function drawGrid() {
        ctx.strokeStyle = GRID_COLOR;
        ctx.lineWidth = 0.5;
        for (let i = 0; i <= 4; i++) {
            const y = padding.top + (plotH / 4) * i;
            ctx.beginPath();
            ctx.moveTo(padding.left, y);
            ctx.lineTo(padding.left + plotW, y);
            ctx.stroke();
        }
        const labelCount = Math.min(8, data.length);
        const step = Math.max(1, Math.floor(data.length / labelCount));
        ctx.fillStyle = TEXT_COLOR;
        ctx.font = "10px 'JetBrains Mono', monospace";
        ctx.textAlign = "center";
        for (let i = 0; i < data.length; i += step) {
            const x = xPos(data[i].timestamp);
            const secondsAgo = Math.round(tMax - data[i].timestamp);
            ctx.fillText(`-${secondsAgo}s`, x, height - 8);
        }
        ctx.textAlign = "right";
        for (let i = 0; i <= 4; i++) {
            const y = padding.top + (plotH / 4) * i;
            const val = leftMax - (leftMax / 4) * i;
            ctx.fillText(`${Math.round(val)}`, padding.left - 8, y + 4);
        }
    }
    drawGrid();
    ctx.strokeStyle = GRID_COLOR;
    ctx.lineWidth = 0.5;
    ctx.strokeRect(padding.left, padding.top, plotW, plotH);
    if (ts !== null && ts >= tMin && ts <= tMax) {
        const cx = xPos(ts);
        ctx.save();
        ctx.beginPath();
        ctx.strokeStyle = CROSSHAIR_COLOR;
        ctx.lineWidth = 1;
        ctx.setLineDash([4, 4]);
        ctx.moveTo(cx, padding.top);
        ctx.lineTo(cx, padding.top + plotH);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.restore();
        const closest = data.reduce((prev, curr) => Math.abs(curr.timestamp - ts) < Math.abs(prev.timestamp - ts)
            ? curr
            : prev);
        const tooltipX = Math.min(cx + 12, padding.left + plotW - 200);
        const tooltipY = padding.top + 8;
        ctx.fillStyle = "rgba(9, 9, 11, 0.92)";
        ctx.strokeStyle = "rgba(113, 113, 122, 0.6)";
        ctx.lineWidth = 1;
        const tooltipW = 190;
        const tooltipH = 88;
        ctx.beginPath();
        ctx.roundRect(tooltipX, tooltipY, tooltipW, tooltipH, 4);
        ctx.fill();
        ctx.stroke();
        ctx.font = "10px 'JetBrains Mono', monospace";
        ctx.textAlign = "left";
        const lines = [
            { label: CPU_LABEL, value: `${closest.seriesData.cpuUtilization.toFixed(1)}%`, color: CPU_COLOR },
            { label: MEM_LABEL, value: `${closest.seriesData.memoryUsageMb.toFixed(0)}%`, color: MEM_COLOR },
            { label: SM_LABEL, value: `${closest.seriesData.gpuSmUtilization.toFixed(1)}%`, color: SM_COLOR },
            { label: VRAM_LABEL, value: `${closest.seriesData.gpuVramAllocatedMb.toFixed(0)} MB`, color: VRAM_COLOR },
        ];
        lines.forEach((line, i) => {
            const ly = tooltipY + 16 + i * 18;
            ctx.fillStyle = line.color;
            ctx.fillText(line.label, tooltipX + 8, ly);
            ctx.textAlign = "right";
            ctx.fillStyle = TEXT_COLOR;
            ctx.fillText(line.value, tooltipX + tooltipW - 8, ly);
            ctx.textAlign = "left";
        });
    }
    ctx.restore();
}
export function SystemMetricsChart({ buffer, version, height = 280, }) {
    const canvasRef = useRef(null);
    const containerRef = useRef(null);
    const dataRef = useRef([]);
    const hoverRef = useRef(null);
    const rafRef = useRef(null);
    dataRef.current = buffer.getSnapshot();
    const scheduleDraw = useCallback(() => {
        if (rafRef.current === null) {
            rafRef.current = requestAnimationFrame(() => {
                rafRef.current = null;
                const canvas = canvasRef.current;
                if (!canvas)
                    return;
                const container = containerRef.current;
                if (!container)
                    return;
                const dpr = window.devicePixelRatio || 1;
                const rect = container.getBoundingClientRect();
                canvas.width = rect.width * dpr;
                canvas.height = height * dpr;
                canvas.style.width = `${rect.width}px`;
                canvas.style.height = `${height}px`;
                const ctx = canvas.getContext("2d");
                if (!ctx)
                    return;
                drawChart(ctx, {
                    data: dataRef.current,
                    hoverTs: hoverRef.current,
                    dpr,
                    width: rect.width,
                    height,
                });
            });
        }
    }, [height]);
    useEffect(() => {
        scheduleDraw();
    }, [version, scheduleDraw]);
    useEffect(() => {
        const unsub = subscribeGlobalCrosshair(() => {
            hoverRef.current = getGlobalCrosshair();
            scheduleDraw();
        });
        return unsub;
    }, [scheduleDraw]);
    useEffect(() => {
        const container = containerRef.current;
        if (!container)
            return;
        const ro = new ResizeObserver(() => scheduleDraw());
        ro.observe(container);
        return () => ro.disconnect();
    }, [scheduleDraw]);
    const handleMouseMove = useCallback((e) => {
        const canvas = canvasRef.current;
        const container = containerRef.current;
        if (!canvas || !container)
            return;
        const rect = container.getBoundingClientRect();
        const mouseX = e.clientX - rect.left;
        const data = dataRef.current;
        if (data.length < 2)
            return;
        const padding = { top: 24, bottom: 32, left: 56, right: 16 };
        const plotW = rect.width - padding.left - padding.right;
        const tMin = data[0].timestamp;
        const tMax = data[data.length - 1].timestamp;
        const tRange = Math.max(tMax - tMin, 1);
        const fraction = (mouseX - padding.left) / plotW;
        const ts = tMin + fraction * tRange;
        const clamped = Math.max(tMin, Math.min(tMax, ts));
        setGlobalCrosshair(clamped);
    }, []);
    function handleMouseLeave() {
        setGlobalCrosshair(null);
    }
    return (_jsx("div", { ref: containerRef, className: "relative", children: _jsx("canvas", { ref: canvasRef, className: "block w-full cursor-crosshair", style: { height }, onMouseMove: handleMouseMove, onMouseLeave: handleMouseLeave }) }));
}
