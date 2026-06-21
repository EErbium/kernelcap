import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMitigationStore } from "../hooks/useMitigationStore";
const ICONS = {
    success: "\u2705",
    error: "\u274C",
    warning: "\u26A0\uFE0F",
    info: "\u2139\uFE0F",
};
const STYLES = {
    success: "border-emerald-700/40 bg-emerald-950/80 text-emerald-200",
    error: "border-rose-700/40 bg-rose-950/80 text-rose-200",
    warning: "border-amber-700/40 bg-amber-950/80 text-amber-200",
    info: "border-cyan-700/40 bg-cyan-950/80 text-cyan-200",
};
export function ToastNotification() {
    const toasts = useMitigationStore((s) => s.toasts);
    const dismissToast = useMitigationStore((s) => s.dismissToast);
    if (toasts.length === 0)
        return null;
    return (_jsx("div", { className: "fixed bottom-4 right-4 z-50 flex flex-col-reverse gap-2 max-w-sm", "aria-live": "polite", children: toasts.map((t) => (_jsxs("div", { className: `flex items-start gap-2.5 px-4 py-3 rounded-lg shadow-lg border font-mono text-sm animate-toast-slide-in ${STYLES[t.type]}`, children: [_jsx("span", { className: "text-base leading-none shrink-0", children: ICONS[t.type] }), _jsx("span", { className: "flex-1 text-[12px] leading-snug", children: t.message }), _jsx("button", { onClick: () => dismissToast(t.id), className: "text-zinc-500 hover:text-zinc-300 transition-colors text-[14px] leading-none shrink-0", "aria-label": "Dismiss", children: "\\u2715" })] }, t.id))) }));
}
