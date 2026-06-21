import { useRef, useEffect, useCallback } from "react";
import type { ScatterPoint } from "../types/charting";

import {
  setGlobalCrosshair,
  subscribeGlobalCrosshair,
  getGlobalCrosshair,
} from "../hooks/useSynchronizedCrosshair";

interface EfficiencyScatterPlotProps {
  points: ScatterPoint[];
  height?: number;
}

const BG_COLOR = "#09090b";
const GRID_COLOR = "rgba(63, 63, 70, 0.4)";
const TEXT_COLOR = "rgba(161, 161, 170, 0.8)";
const DANGER_ZONE_COLOR = "rgba(225, 29, 72, 0.12)";
const DANGER_ZONE_BORDER = "rgba(225, 29, 72, 0.5)";

const POINT_COLORS: Record<string, string> = {
  HEALTHY: "#22d3ee",
  SEMANTIC_LOOP: "#fbbf24",
  IDLE_HOG: "#f43f5e",
};

const SM_THRESHOLD = 30;
const VRAM_THRESHOLD_PCT = 0.6;

interface DrawState {
  points: ScatterPoint[];
  hoverTs: number | null;
  dpr: number;
  width: number;
  height: number;
}

function drawScatter(
  ctx: CanvasRenderingContext2D,
  state: DrawState
) {
  const { points, dpr, width, height, hoverTs } = state;
  const padding = { top: 24, right: 20, bottom: 32, left: 64 };
  const plotW = width - padding.left - padding.right;
  const plotH = height - padding.top - padding.bottom;

  ctx.save();
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, width, height);

  const xMin = 0;
  const xMax = 100;
  const vramValues = points.map((p) => p.vramAllocatedMb);
  const yMin = 0;
  const yMax = Math.max(...vramValues, 1024) * 1.15;

  function xPos(v: number): number {
    return padding.left + ((v - xMin) / (xMax - xMin)) * plotW;
  }
  function yPos(v: number): number {
    return padding.top + plotH - ((v - yMin) / (yMax - yMin)) * plotH;
  }

  ctx.fillStyle = BG_COLOR;
  ctx.fillRect(0, 0, width, height);

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
    for (let i = 0; i <= 4; i++) {
      const x = padding.left + (plotW / 4) * i;
      ctx.beginPath();
      ctx.moveTo(x, padding.top);
      ctx.lineTo(x, padding.top + plotH);
      ctx.stroke();
    }

    ctx.fillStyle = TEXT_COLOR;
    ctx.font = "10px 'JetBrains Mono', monospace";

    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    for (let i = 0; i <= 4; i++) {
      const x = padding.left + (plotW / 4) * i;
      ctx.fillText(`${Math.round(xMin + (xMax / 4) * i)}`, x, height - 20);
    }

    ctx.textAlign = "right";
    ctx.textBaseline = "middle";
    for (let i = 0; i <= 4; i++) {
      const y = padding.top + (plotH / 4) * i;
      const val = yMax - (yMax / 4) * i;
      ctx.fillText(`${Math.round(val)}`, padding.left - 8, y);
    }
  }

  drawGrid();

  const quadrantX = xPos(SM_THRESHOLD);
  const quadrantY = yPos(yMax * VRAM_THRESHOLD_PCT);

  ctx.save();
  ctx.fillStyle = DANGER_ZONE_COLOR;
  ctx.fillRect(padding.left, quadrantY, quadrantX - padding.left, padding.top + plotH - quadrantY);

  ctx.strokeStyle = DANGER_ZONE_BORDER;
  ctx.lineWidth = 1;
  ctx.setLineDash([4, 3]);
  ctx.beginPath();
  ctx.moveTo(padding.left, quadrantY);
  ctx.lineTo(quadrantX, quadrantY);
  ctx.lineTo(quadrantX, padding.top + plotH);
  ctx.stroke();
  ctx.setLineDash([]);

  ctx.fillStyle = DANGER_ZONE_BORDER;
  ctx.font = "9px 'JetBrains Mono', monospace";
  ctx.textAlign = "left";
  ctx.textBaseline = "bottom";
  ctx.fillText("IDLE GPU HOG RISK", padding.left + 6, quadrantY - 4);
  ctx.restore();

  ctx.save();
  ctx.strokeStyle = GRID_COLOR;
  ctx.lineWidth = 0.5;
  ctx.strokeRect(padding.left, padding.top, plotW, plotH);
  ctx.restore();

  if (points.length === 0) {
    ctx.fillStyle = TEXT_COLOR;
    ctx.font = "13px 'JetBrains Mono', monospace";
    ctx.textAlign = "center";
    ctx.fillText("Waiting for data...", width / 2, height / 2);
    ctx.restore();
    return;
  }

  const hoveredPts: ScatterPoint[] = [];
  const hTs = hoverTs;

  for (const pt of points) {
    const px = xPos(pt.smUtilizationPct);
    const py = yPos(pt.vramAllocatedMb);
    const color = POINT_COLORS[pt.anomalyStatus] ?? "#a1a1aa";
    const radius = pt.anomalyStatus !== "HEALTHY" ? 5 : 3.5;

    ctx.beginPath();
    ctx.arc(px, py, radius, 0, Math.PI * 2);
    ctx.fillStyle = color;
    ctx.globalAlpha = 0.85;
    ctx.fill();
    ctx.globalAlpha = 0.3;
    ctx.arc(px, py, radius * 2, 0, Math.PI * 2);
    ctx.fill();
    ctx.globalAlpha = 1;

    if (
      hTs !== null &&
      Math.abs(pt.timestamp - hTs) < 1.5
    ) {
      hoveredPts.push(pt);
      ctx.beginPath();
      ctx.arc(px, py, 7, 0, Math.PI * 2);
      ctx.strokeStyle = "#ffffff";
      ctx.lineWidth = 1.5;
      ctx.stroke();
    }
  }

  if (hoveredPts.length > 0) {
    const pt = hoveredPts[0];
    const tx = Math.min(xPos(pt.smUtilizationPct) + 16, width - 180);
    const ty = padding.top + 8;
    ctx.fillStyle = "rgba(9, 9, 11, 0.92)";
    ctx.strokeStyle = "rgba(113, 113, 122, 0.6)";
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.roundRect(tx, ty, 170, 72, 4);
    ctx.fill();
    ctx.stroke();
    ctx.font = "10px 'JetBrains Mono', monospace";
    ctx.textAlign = "left";
    ctx.fillStyle = TEXT_COLOR;
    ctx.fillText(pt.label, tx + 8, ty + 16);
    ctx.fillStyle = POINT_COLORS[pt.anomalyStatus] ?? TEXT_COLOR;
    ctx.fillText(
      `SM: ${pt.smUtilizationPct.toFixed(1)}%  VRAM: ${pt.vramAllocatedMb.toFixed(0)}MB`,
      tx + 8,
      ty + 34
    );
    ctx.fillStyle = "#71717a";
    const d = new Date(pt.timestamp * 1000);
    ctx.fillText(d.toLocaleTimeString(), tx + 8, ty + 52);
  }

  ctx.restore();
}

export function EfficiencyScatterPlot({
  points,
  height = 280,
}: EfficiencyScatterPlotProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const hoverRef = useRef<number | null>(null);
  const rafRef = useRef<ReturnType<typeof requestAnimationFrame> | null>(null);
  const ptsRef = useRef(points);
  ptsRef.current = points;

  const scheduleDraw = useCallback(() => {
    if (rafRef.current === null) {
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = null;
        const canvas = canvasRef.current;
        if (!canvas) return;
        const container = containerRef.current;
        if (!container) return;
        const dpr = window.devicePixelRatio || 1;
        const rect = container.getBoundingClientRect();
        canvas.width = rect.width * dpr;
        canvas.height = height * dpr;
        canvas.style.width = `${rect.width}px`;
        canvas.style.height = `${height}px`;
        const ctx = canvas.getContext("2d");
        if (!ctx) return;
        drawScatter(ctx, {
          points: ptsRef.current,
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
  });

  useEffect(() => {
    const unsub = subscribeGlobalCrosshair(() => {
      hoverRef.current = getGlobalCrosshair();
      scheduleDraw();
    });
    return unsub;
  }, [scheduleDraw]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const ro = new ResizeObserver(() => scheduleDraw());
    ro.observe(container);
    return () => ro.disconnect();
  }, [scheduleDraw]);

  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLCanvasElement>) => {
      const canvas = canvasRef.current;
      if (!canvas) return;
      const rect = canvas.getBoundingClientRect();
      const pts = ptsRef.current;
      if (pts.length === 0) return;
      const padding = { top: 24, bottom: 32, left: 64, right: 20 };
      const plotW = rect.width - padding.left - padding.right;
      const mouseX = e.clientX - rect.left;
      const xMin = 0;
      const xMax = 100;
      const fraction = (mouseX - padding.left) / plotW;
      const smVal = xMin + fraction * xMax;
      const closest = pts.reduce((prev, curr) =>
        Math.abs(curr.smUtilizationPct - smVal) <
        Math.abs(prev.smUtilizationPct - smVal)
          ? curr
          : prev
      );
      setGlobalCrosshair(closest.timestamp);
    },
    []
  );

  function handleMouseLeave() {
    setGlobalCrosshair(null);
  }

  return (
    <div ref={containerRef} className="relative">
      <canvas
        ref={canvasRef}
        className="block w-full cursor-crosshair"
        style={{ height }}
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
      />
    </div>
  );
}
