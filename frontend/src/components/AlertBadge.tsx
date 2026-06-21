import type { AnomalyStatus } from "../types/metrics";


interface AlertBadgeProps {
  status: AnomalyStatus;
}

const BADGE_CONFIG: Record<
  AnomalyStatus,
  { label: string; classes: string; pulse: boolean }
> = {
  HEALTHY: {
    label: "OK",
    classes: "bg-emerald-900/60 text-emerald-300 border-emerald-700/50",
    pulse: false,
  },
  SEMANTIC_LOOP: {
    label: "LOOP",
    classes: "bg-amber-900/60 text-amber-300 border-amber-600/50",
    pulse: true,
  },
  IDLE_HOG: {
    label: "IDLE",
    classes: "bg-rose-900/60 text-rose-300 border-rose-600/50",
    pulse: true,
  },
};

const PULSE_CLASS = "animate-pulse-ring";

export function AlertBadge({ status }: AlertBadgeProps) {
  const cfg = BADGE_CONFIG[status];
  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono font-semibold border leading-none whitespace-nowrap ${cfg.classes} ${
        cfg.pulse ? PULSE_CLASS : ""
      }`}
    >
      <span
        className={`inline-block w-1.5 h-1.5 rounded-full ${
          status === "HEALTHY"
            ? "bg-emerald-400"
            : status === "SEMANTIC_LOOP"
            ? "bg-amber-400"
            : "bg-rose-400"
        }`}
      />
      {cfg.label}
    </span>
  );
}
