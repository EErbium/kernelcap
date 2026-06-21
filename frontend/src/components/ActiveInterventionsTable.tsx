import { useState, useEffect, useCallback } from "react";
import type {
  ActiveInterventionNode,
  MitigationStatus,
  OverrideAction,
  UserProfile,
} from "../types/mitigation";

import { RBACGuard } from "./RBACGuard";
import { ActionGateModal } from "./ActionGateModal";

interface ActiveInterventionsTableProps {
  interventions: ActiveInterventionNode[];
  userProfile: UserProfile;
  onExecute: (
    mitigationId: string,
    actionType: OverrideAction
  ) => Promise<boolean>;
  pendingMap: ReadonlyMap<string, boolean>;
}

const STATUS_CONFIG: Record<
  MitigationStatus,
  { label: string; classes: string; pulse: boolean }
> = {
  ACTIVE_ENFORCED: {
    label: "ACTIVE",
    classes: "bg-amber-900/40 text-amber-300 border-amber-700/50",
    pulse: true,
  },
  PENDING_VERIFICATION: {
    label: "PENDING",
    classes: "bg-cyan-900/40 text-cyan-300 border-cyan-700/50",
    pulse: true,
  },
  ROLLBACK_FAILED: {
    label: "FAILED",
    classes: "bg-rose-900/40 text-rose-300 border-rose-700/50",
    pulse: true,
  },
  RESOLVED: {
    label: "RESOLVED",
    classes: "bg-emerald-900/30 text-emerald-400 border-emerald-700/30",
    pulse: false,
  },
};

const ACTION_LABELS: Record<string, string> = {
  SIGSTOP_FREEZE: "SIGNAL STOP",
  CONTAINER_PAUSE: "CONTAINER PAUSE",
  API_REROUTE: "API REROUTE",
};

function formatElapsed(ts: number): string {
  const sec = Math.floor(Date.now() / 1000) - ts;
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return `${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
}

function elapsedKey(ts: number): string {
  const sec = Math.floor(Date.now() / 1000) - ts;
  return `${Math.floor(sec / 60)}:${(sec % 60).toString().padStart(2, "0")}`;
}

export function ActiveInterventionsTable({
  interventions,
  userProfile,
  onExecute,
  pendingMap,
}: ActiveInterventionsTableProps) {
  const [tick, setTick] = useState(0);
  const [modalState, setModalState] = useState<{
    intervention: ActiveInterventionNode;
    actionType: OverrideAction;
  } | null>(null);

  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, []);

  const handleResume = useCallback(
    (intervention: ActiveInterventionNode) => {
      setModalState({ intervention, actionType: "FORCE_RESUME" });
    },
    []
  );

  const handleTerminate = useCallback(
    (intervention: ActiveInterventionNode) => {
      setModalState({ intervention, actionType: "FORCE_TERMINATE" });
    },
    []
  );

  const handleModalConfirm = useCallback(
    async (id: string, action: OverrideAction) => {
      await onExecute(id, action);
      setModalState(null);
    },
    [onExecute]
  );

  if (interventions.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-zinc-600 text-sm font-mono">
        No active interventions
      </div>
    );
  }

  return (
    <>
      <div className="overflow-x-auto">
        <table className="w-full text-[13px] font-mono">
          <thead>
            <tr className="text-[11px] font-semibold text-zinc-500 uppercase tracking-wider border-b border-zinc-800/80 bg-zinc-900/50">
              <th className="text-left px-3 py-2 font-normal">Node</th>
              <th className="text-left px-2 py-2 font-normal">PID</th>
              <th className="text-left px-2 py-2 font-normal">Process</th>
              <th className="text-left px-2 py-2 font-normal">Action</th>
              <th className="text-right px-2 py-2 font-normal">Duration</th>
              <th className="text-left px-2 py-2 font-normal">Status</th>
              <th className="text-left px-2 py-2 font-normal max-w-[200px]">Violation</th>
              <th className="text-right px-3 py-2 font-normal">Controls</th>
            </tr>
          </thead>
          <tbody>
            {interventions.map((inv) => {
              const sc = STATUS_CONFIG[inv.currentStatus];
              const isPending =
                inv.currentStatus === "PENDING_VERIFICATION" ||
                pendingMap.has(inv.mitigationId);
              const isResolved = inv.currentStatus === "RESOLVED";

              return (
                <tr
                  key={inv.mitigationId}
                  className={`border-b border-zinc-800/40 transition-colors ${
                    isResolved
                      ? "opacity-50"
                      : "hover:bg-zinc-800/20"
                  }`}
                >
                  <td className="px-3 py-2.5 text-zinc-300 truncate max-w-[140px]" title={inv.nodeId}>
                    {inv.nodeId}
                  </td>
                  <td className="px-2 py-2.5 tabular-nums text-zinc-100">
                    {inv.targetPid}
                  </td>
                  <td className="px-2 py-2.5 text-zinc-100 truncate max-w-[130px]" title={inv.processName}>
                    {inv.processName}
                  </td>
                  <td className="px-2 py-2.5">
                    <span className="inline-flex px-2 py-0.5 rounded text-[11px] bg-zinc-800/60 text-zinc-300 border border-zinc-700/40">
                      {ACTION_LABELS[inv.appliedAction] ?? inv.appliedAction}
                    </span>
                  </td>
                  <td className="px-2 py-2.5 text-right tabular-nums text-zinc-400">
                    {isResolved ? (
                      <span className="text-zinc-600">
                        {formatElapsed(inv.executionTimestamp)}
                      </span>
                    ) : (
                      <span className="text-zinc-300" key={`${tick}-${inv.mitigationId}`}>
                        {elapsedKey(inv.executionTimestamp)}
                      </span>
                    )}
                  </td>
                  <td className="px-2 py-2.5">
                    <span
                      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[11px] font-semibold border leading-none ${
                        sc.classes
                      } ${sc.pulse && !isResolved ? "animate-pulse" : ""}`}
                    >
                      {isPending && (
                        <span className="inline-block w-3 h-3 border-2 border-current border-t-transparent rounded-full animate-spin" />
                      )}
                      {sc.label}
                    </span>
                  </td>
                  <td className="px-2 py-2.5 text-zinc-500 text-[12px] truncate max-w-[200px]" title={inv.policyViolationReason}>
                    {inv.policyViolationReason}
                  </td>
                  <td className="px-3 py-2.5 text-right">
                    <div className="flex items-center justify-end gap-1.5">
                      <RBACGuard
                        requiredRole="Operator"
                        currentRole={userProfile.role}
                        tooltip="Operator or Admin role required to resume"
                      >
                        <button
                          onClick={() => handleResume(inv)}
                          disabled={isPending || isResolved}
                          className={`px-2.5 py-1 rounded text-[11px] font-semibold transition-all ${
                            isPending || isResolved
                              ? "text-zinc-700 bg-zinc-900 cursor-not-allowed"
                              : "text-emerald-400 bg-emerald-950/50 hover:bg-emerald-950/80 border border-emerald-800/40 hover:border-emerald-700/60"
                          }`}
                        >
                          Resume
                        </button>
                      </RBACGuard>

                      <RBACGuard
                        requiredRole="Admin"
                        currentRole={userProfile.role}
                        tooltip="Admin role required to terminate"
                      >
                        <button
                          onClick={() => handleTerminate(inv)}
                          disabled={isPending || isResolved}
                          className={`px-2.5 py-1 rounded text-[11px] font-semibold transition-all ${
                            isPending || isResolved
                              ? "text-zinc-700 bg-zinc-900 cursor-not-allowed"
                              : "text-rose-400 bg-rose-950/50 hover:bg-rose-950/80 border border-rose-800/40 hover:border-rose-700/60"
                          }`}
                        >
                          Terminate
                        </button>
                      </RBACGuard>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {modalState && (
        <ActionGateModal
          intervention={modalState.intervention}
          actionType={modalState.actionType}
          onConfirm={handleModalConfirm}
          onClose={() => setModalState(null)}
          isPending={
            pendingMap.has(modalState.intervention.mitigationId)
          }
        />
      )}
    </>
  );
}
