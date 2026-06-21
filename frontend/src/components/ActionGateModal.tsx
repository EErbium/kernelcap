import { useState, useCallback, useRef } from "react";
import type {
  ActiveInterventionNode,
  OverrideAction,
} from "../types/mitigation";


interface ActionGateModalProps {
  intervention: ActiveInterventionNode;
  actionType: OverrideAction;
  onConfirm: (mitigationId: string, actionType: OverrideAction) => void;
  onClose: () => void;
  isPending: boolean;
}

const STAGE_LABELS = ["Warning", "Confirmation", "Executing"] as const;

const ACTION_DESCRIPTIONS: Record<OverrideAction, string> = {
  FORCE_RESUME:
    "This will send a SIGCONT signal to the paused process, allowing it to resume normal execution. The process may immediately re-enter an anomaly state if the root cause is unresolved.",
  FORCE_TERMINATE:
    "This will send a SIGTERM followed by SIGKILL to the target process. All unsaved inference state, cached tokens, and GPU memory allocations will be permanently lost.",
};

export function ActionGateModal({
  intervention,
  actionType,
  onConfirm,
  onClose,
  isPending,
}: ActionGateModalProps) {
  const [stage, setStage] = useState(0);
  const [typedPid, setTypedPid] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

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

  const handleBackdrop = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget && stage < 2) {
        onClose();
      }
    },
    [stage, onClose]
  );

  return (
    <div
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={handleBackdrop}
    >
      <div className="bg-zinc-900 border border-zinc-700/60 rounded-xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b border-zinc-800">
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1.5 text-[11px] font-mono text-zinc-500">
              {STAGE_LABELS.map((label, i) => (
                <span key={label} className="flex items-center gap-1.5">
                  {i > 0 && <span className="text-zinc-700">&rarr;</span>}
                  <span
                    className={`${
                      i === stage
                        ? "text-zinc-200 font-semibold"
                        : i < stage
                        ? "text-emerald-500"
                        : "text-zinc-600"
                    }`}
                  >
                    {label}
                  </span>
                </span>
              ))}
            </span>
          </div>
          {stage < 2 && (
            <button
              onClick={onClose}
              className="text-zinc-600 hover:text-zinc-300 text-lg leading-none"
              aria-label="Close"
            >
              \u2715
            </button>
          )}
        </div>

        <div className="px-5 py-5">
          {stage === 0 && (
            <div className="space-y-4">
              <div className="flex items-start gap-3">
                <span className="text-2xl shrink-0">\u26A0\uFE0F</span>
                <div>
                  <h3 className="text-base font-semibold text-amber-300 mb-1">
                    Destructive Action Required
                  </h3>
                  <p className="text-zinc-400 text-[13px] leading-relaxed">
                    You are about to <strong className="text-zinc-200">{actionType === "FORCE_RESUME" ? "force resume" : "force terminate"}</strong> process{" "}
                    <code className="text-zinc-100 bg-zinc-800 px-1.5 py-0.5 rounded text-[12px]">
                      PID {intervention.targetPid}
                    </code>{" "}
                    ({intervention.processName}) on node{" "}
                    <code className="text-zinc-100 bg-zinc-800 px-1.5 py-0.5 rounded text-[12px]">
                      {intervention.nodeId}
                    </code>.
                  </p>
                </div>
              </div>
              <div className="bg-rose-950/30 border border-rose-800/40 rounded-lg px-4 py-3 text-zinc-300 text-[12px] leading-relaxed">
                {ACTION_DESCRIPTIONS[actionType]}
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <button
                  onClick={onClose}
                  className="px-4 py-2 rounded-lg text-[13px] text-zinc-400 hover:text-zinc-200 bg-zinc-800 hover:bg-zinc-700 transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={handleAcknowledge}
                  className="px-4 py-2 rounded-lg text-[13px] font-semibold bg-amber-600 hover:bg-amber-500 text-white transition-colors"
                >
                  Acknowledge & Continue
                </button>
              </div>
            </div>
          )}

          {stage === 1 && (
            <div className="space-y-4">
              <div className="flex items-start gap-3">
                <span className="text-2xl shrink-0">\u270D\uFE0F</span>
                <div>
                  <h3 className="text-base font-semibold text-zinc-200 mb-1">
                    Confirm Target PID
                  </h3>
                  <p className="text-zinc-400 text-[13px]">
                    Type <code className="text-zinc-100 bg-zinc-800 px-1.5 py-0.5 rounded font-mono text-[12px]">{intervention.targetPid}</code> below to confirm this action.
                  </p>
                </div>
              </div>
              <input
                ref={inputRef}
                type="text"
                value={typedPid}
                onChange={(e) => setTypedPid(e.target.value)}
                placeholder="Enter PID to confirm..."
                className="w-full px-3 py-2.5 rounded-lg bg-zinc-800 border border-zinc-700 text-zinc-100 text-[14px] font-mono placeholder:text-zinc-600 focus:outline-none focus:ring-2 focus:ring-amber-600/50"
                autoComplete="off"
                spellCheck={false}
              />
              <div className="flex justify-end gap-2 pt-2">
                <button
                  onClick={() => setStage(0)}
                  className="px-4 py-2 rounded-lg text-[13px] text-zinc-400 hover:text-zinc-200 bg-zinc-800 hover:bg-zinc-700 transition-colors"
                >
                  Back
                </button>
                <button
                  onClick={handleExecute}
                  disabled={!pidMatch}
                  className={`px-4 py-2 rounded-lg text-[13px] font-semibold transition-all ${
                    pidMatch
                      ? actionType === "FORCE_TERMINATE"
                        ? "bg-rose-600 hover:bg-rose-500 text-white"
                        : "bg-emerald-600 hover:bg-emerald-500 text-white"
                      : "bg-zinc-800 text-zinc-600 cursor-not-allowed"
                  }`}
                >
                  {actionType === "FORCE_TERMINATE"
                    ? "Confirm Terminate"
                    : "Confirm Resume"}
                </button>
              </div>
            </div>
          )}

          {stage === 2 && (
            <div className="space-y-4 text-center py-6">
              <span className="inline-block w-10 h-10 border-[3px] border-zinc-600 border-t-cyan-400 rounded-full animate-spin" />
              <p className="text-zinc-300 text-[14px] font-mono">
                {isPending
                  ? "Waiting for server confirmation..."
                  : "Executing override command..."}
              </p>
              <p className="text-zinc-600 text-[12px]">
                {intervention.mitigationId}
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
