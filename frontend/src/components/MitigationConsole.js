import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect } from "react";
import { useMitigationStore } from "../hooks/useMitigationStore";
import { useRemoteCommand } from "../hooks/useRemoteCommand";
import { ActiveInterventionsTable } from "./ActiveInterventionsTable";
import { ToastNotification } from "./ToastNotification";
import { generateSyntheticInterventions } from "../utils/syntheticInterventions";
const SYNTHETIC_COUNT = 10;
let seeded = false;
export function MitigationConsole({ userProfile }) {
    const interventions = useMitigationStore((s) => s.interventions);
    const setInterventions = useMitigationStore((s) => s.setInterventions);
    const { execute, pendingMap } = useRemoteCommand();
    useEffect(() => {
        if (!seeded && interventions.length === 0) {
            seeded = true;
            const synth = generateSyntheticInterventions(SYNTHETIC_COUNT);
            setInterventions(synth);
        }
    }, [interventions.length, setInterventions]);
    let _active = 0, _pending = 0, _failed = 0, _resolved = 0;
    for (const inv of interventions) {
        if (inv.currentStatus === "ACTIVE_ENFORCED")
            _active++;
        else if (inv.currentStatus === "PENDING_VERIFICATION")
            _pending++;
        else if (inv.currentStatus === "ROLLBACK_FAILED")
            _failed++;
        else if (inv.currentStatus === "RESOLVED")
            _resolved++;
    }
    const stats = { active: _active, pending: _pending, failed: _failed, resolved: _resolved, total: interventions.length };
    return (_jsxs("div", { className: "flex flex-col h-full bg-zinc-950 text-zinc-100", children: [_jsxs("div", { className: "flex items-center justify-between px-4 py-2 border-b border-zinc-800 bg-zinc-900/80 shrink-0", children: [_jsx("h2", { className: "text-sm font-semibold tracking-wide text-zinc-200 uppercase", children: "Mitigation Override Center" }), _jsxs("div", { className: "flex items-center gap-2 text-[10px] font-mono", children: [_jsx("span", { className: "px-2 py-0.5 rounded bg-zinc-800 text-zinc-400 border border-zinc-700/40", children: userProfile.role }), _jsx("span", { className: "text-zinc-600 truncate max-w-[160px]", title: userProfile.email, children: userProfile.email || userProfile.userId })] })] }), _jsxs("div", { className: "flex items-center gap-4 px-4 py-2 border-b border-zinc-800/40 bg-zinc-900/40 shrink-0", children: [_jsx("span", { className: "text-[11px] text-zinc-500 font-semibold uppercase tracking-wider", children: "Summary" }), _jsxs("div", { className: "flex items-center gap-3", children: [_jsxs("span", { className: "flex items-center gap-1.5 text-[12px]", children: [_jsx("span", { className: "inline-block w-2 h-2 rounded-full bg-amber-400" }), _jsx("span", { className: "text-zinc-300", children: stats.active }), _jsx("span", { className: "text-zinc-600", children: "active" })] }), _jsxs("span", { className: "flex items-center gap-1.5 text-[12px]", children: [_jsx("span", { className: "inline-block w-2 h-2 rounded-full bg-cyan-400" }), _jsx("span", { className: "text-zinc-300", children: stats.pending }), _jsx("span", { className: "text-zinc-600", children: "pending" })] }), _jsxs("span", { className: "flex items-center gap-1.5 text-[12px]", children: [_jsx("span", { className: "inline-block w-2 h-2 rounded-full bg-rose-400" }), _jsx("span", { className: "text-zinc-300", children: stats.failed }), _jsx("span", { className: "text-zinc-600", children: "failed" })] }), _jsxs("span", { className: "flex items-center gap-1.5 text-[12px]", children: [_jsx("span", { className: "inline-block w-2 h-2 rounded-full bg-emerald-500" }), _jsx("span", { className: "text-zinc-300", children: stats.resolved }), _jsx("span", { className: "text-zinc-600", children: "resolved" })] }), _jsx("span", { className: "text-zinc-700", children: "|" }), _jsxs("span", { className: "text-zinc-600 text-[12px]", children: [stats.total, " total"] })] })] }), _jsx("div", { className: "flex-1 overflow-y-auto", children: _jsx(ActiveInterventionsTable, { interventions: interventions, userProfile: userProfile, onExecute: execute, pendingMap: pendingMap }) }), _jsx(ToastNotification, {})] }));
}
