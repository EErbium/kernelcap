import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState, useCallback, useRef } from "react";
const STAGE_LABELS = ["Warning", "Confirmation", "Executing"];
const ACTION_DESCRIPTIONS = {
    FORCE_RESUME: "This will send a SIGCONT signal to the paused process, allowing it to resume normal execution. The process may immediately re-enter an anomaly state if the root cause is unresolved.",
    FORCE_TERMINATE: "This will send a SIGTERM followed by SIGKILL to the target process. All unsaved inference state, cached tokens, and GPU memory allocations will be permanently lost.",
};
export function ActionGateModal({ intervention, actionType, onConfirm, onClose, isPending, }) {
    const [stage, setStage] = useState(0);
    const [typedPid, setTypedPid] = useState("");
    const inputRef = useRef(null);
    const targetPidStr = String(intervention.targetPid);
    const pidMatch = typedPid === targetPidStr;
    const handleAcknowledge = useCallback(() => {
        setStage(1);
        setTimeout(() => inputRef.current?.focus(), 50);
    }, []);
    const handleExecute = useCallback(() => {
        setStage(2);
        onConfirm(intervention.mitigationId, actionType);
    }, [intervention.mitigationId, actionType, onConfirm]);
    const handleBackdrop = useCallback((e) => {
        if (e.target === e.currentTarget && stage < 2) {
            onClose();
        }
    }, [stage, onClose]);
    return (_jsx("div", { className: "fixed inset-0 z-40 flex items-center justify-center bg-black/60 backdrop-blur-sm", onClick: handleBackdrop, children: _jsxs("div", { className: "bg-zinc-900 border border-zinc-700/60 rounded-xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden", children: [_jsxs("div", { className: "flex items-center justify-between px-5 py-3 border-b border-zinc-800", children: [_jsx("div", { className: "flex items-center gap-3", children: _jsx("span", { className: "flex items-center gap-1.5 text-[11px] font-mono text-zinc-500", children: STAGE_LABELS.map((label, i) => (_jsxs("span", { className: "flex items-center gap-1.5", children: [i > 0 && _jsx("span", { className: "text-zinc-700", children: "\u2192" }), _jsx("span", { className: `${i === stage
                                                ? "text-zinc-200 font-semibold"
                                                : i < stage
                                                    ? "text-emerald-500"
                                                    : "text-zinc-600"}`, children: label })] }, label))) }) }), stage < 2 && (_jsx("button", { onClick: onClose, className: "text-zinc-600 hover:text-zinc-300 text-lg leading-none", "aria-label": "Close", children: "\\u2715" }))] }), _jsxs("div", { className: "px-5 py-5", children: [stage === 0 && (_jsxs("div", { className: "space-y-4", children: [_jsxs("div", { className: "flex items-start gap-3", children: [_jsx("span", { className: "text-2xl shrink-0", children: "\\u26A0\\uFE0F" }), _jsxs("div", { children: [_jsx("h3", { className: "text-base font-semibold text-amber-300 mb-1", children: "Destructive Action Required" }), _jsxs("p", { className: "text-zinc-400 text-[13px] leading-relaxed", children: ["You are about to ", _jsx("strong", { className: "text-zinc-200", children: actionType === "FORCE_RESUME" ? "force resume" : "force terminate" }), " process", " ", _jsxs("code", { className: "text-zinc-100 bg-zinc-800 px-1.5 py-0.5 rounded text-[12px]", children: ["PID ", intervention.targetPid] }), " ", "(", intervention.processName, ") on node", " ", _jsx("code", { className: "text-zinc-100 bg-zinc-800 px-1.5 py-0.5 rounded text-[12px]", children: intervention.nodeId }), "."] })] })] }), _jsx("div", { className: "bg-rose-950/30 border border-rose-800/40 rounded-lg px-4 py-3 text-zinc-300 text-[12px] leading-relaxed", children: ACTION_DESCRIPTIONS[actionType] }), _jsxs("div", { className: "flex justify-end gap-2 pt-2", children: [_jsx("button", { onClick: onClose, className: "px-4 py-2 rounded-lg text-[13px] text-zinc-400 hover:text-zinc-200 bg-zinc-800 hover:bg-zinc-700 transition-colors", children: "Cancel" }), _jsx("button", { onClick: handleAcknowledge, className: "px-4 py-2 rounded-lg text-[13px] font-semibold bg-amber-600 hover:bg-amber-500 text-white transition-colors", children: "Acknowledge & Continue" })] })] })), stage === 1 && (_jsxs("div", { className: "space-y-4", children: [_jsxs("div", { className: "flex items-start gap-3", children: [_jsx("span", { className: "text-2xl shrink-0", children: "\\u270D\\uFE0F" }), _jsxs("div", { children: [_jsx("h3", { className: "text-base font-semibold text-zinc-200 mb-1", children: "Confirm Target PID" }), _jsxs("p", { className: "text-zinc-400 text-[13px]", children: ["Type ", _jsx("code", { className: "text-zinc-100 bg-zinc-800 px-1.5 py-0.5 rounded font-mono text-[12px]", children: intervention.targetPid }), " below to confirm this action."] })] })] }), _jsx("input", { ref: inputRef, type: "text", value: typedPid, onChange: (e) => setTypedPid(e.target.value), placeholder: "Enter PID to confirm...", className: "w-full px-3 py-2.5 rounded-lg bg-zinc-800 border border-zinc-700 text-zinc-100 text-[14px] font-mono placeholder:text-zinc-600 focus:outline-none focus:ring-2 focus:ring-amber-600/50", autoComplete: "off", spellCheck: false }), _jsxs("div", { className: "flex justify-end gap-2 pt-2", children: [_jsx("button", { onClick: () => setStage(0), className: "px-4 py-2 rounded-lg text-[13px] text-zinc-400 hover:text-zinc-200 bg-zinc-800 hover:bg-zinc-700 transition-colors", children: "Back" }), _jsx("button", { onClick: handleExecute, disabled: !pidMatch, className: `px-4 py-2 rounded-lg text-[13px] font-semibold transition-all ${pidMatch
                                                ? actionType === "FORCE_TERMINATE"
                                                    ? "bg-rose-600 hover:bg-rose-500 text-white"
                                                    : "bg-emerald-600 hover:bg-emerald-500 text-white"
                                                : "bg-zinc-800 text-zinc-600 cursor-not-allowed"}`, children: actionType === "FORCE_TERMINATE"
                                                ? "Confirm Terminate"
                                                : "Confirm Resume" })] })] })), stage === 2 && (_jsxs("div", { className: "space-y-4 text-center py-6", children: [_jsx("span", { className: "inline-block w-10 h-10 border-[3px] border-zinc-600 border-t-cyan-400 rounded-full animate-spin" }), _jsx("p", { className: "text-zinc-300 text-[14px] font-mono", children: isPending
                                        ? "Waiting for server confirmation..."
                                        : "Executing override command..." }), _jsx("p", { className: "text-zinc-600 text-[12px]", children: intervention.mitigationId })] }))] })] }) }));
}
