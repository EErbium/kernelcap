import { useMitigationStore } from "../hooks/useMitigationStore";
import type { ToastMessage } from "../types/mitigation";



const ICONS: Record<ToastMessage["type"], string> = {
  success: "\u2705",
  error: "\u274C",
  warning: "\u26A0\uFE0F",
  info: "\u2139\uFE0F",
};

const STYLES: Record<ToastMessage["type"], string> = {
  success: "border-emerald-700/40 bg-emerald-950/80 text-emerald-200",
  error: "border-rose-700/40 bg-rose-950/80 text-rose-200",
  warning: "border-amber-700/40 bg-amber-950/80 text-amber-200",
  info: "border-cyan-700/40 bg-cyan-950/80 text-cyan-200",
};

export function ToastNotification() {
  const toasts = useMitigationStore((s) => s.toasts);
  const dismissToast = useMitigationStore((s) => s.dismissToast);

  if (toasts.length === 0) return null;

  return (
    <div
      className="fixed bottom-4 right-4 z-50 flex flex-col-reverse gap-2 max-w-sm"
      aria-live="polite"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`flex items-start gap-2.5 px-4 py-3 rounded-lg shadow-lg border font-mono text-sm animate-toast-slide-in ${
            STYLES[t.type]
          }`}
        >
          <span className="text-base leading-none shrink-0">{ICONS[t.type]}</span>
          <span className="flex-1 text-[12px] leading-snug">{t.message}</span>
          <button
            onClick={() => dismissToast(t.id)}
            className="text-zinc-500 hover:text-zinc-300 transition-colors text-[14px] leading-none shrink-0"
            aria-label="Dismiss"
          >
            \u2715
          </button>
        </div>
      ))}
    </div>
  );
}
