import { useEffect } from "react";
import type { UserProfile } from "../types/mitigation";
import { useMitigationStore } from "../hooks/useMitigationStore";
import { useRemoteCommand } from "../hooks/useRemoteCommand";
import { ActiveInterventionsTable } from "./ActiveInterventionsTable";
import { ToastNotification } from "./ToastNotification";
import { generateSyntheticInterventions } from "../utils/syntheticInterventions";

interface MitigationConsoleProps {
  userProfile: UserProfile;
}

const SYNTHETIC_COUNT = 10;

let seeded = false;

export function MitigationConsole({ userProfile }: MitigationConsoleProps) {
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
    if (inv.currentStatus === "ACTIVE_ENFORCED") _active++;
    else if (inv.currentStatus === "PENDING_VERIFICATION") _pending++;
    else if (inv.currentStatus === "ROLLBACK_FAILED") _failed++;
    else if (inv.currentStatus === "RESOLVED") _resolved++;
  }
  const stats = { active: _active, pending: _pending, failed: _failed, resolved: _resolved, total: interventions.length };

  return (
    <div className="flex flex-col h-full bg-zinc-950 text-zinc-100">
      <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-800 bg-zinc-900/80 shrink-0">
        <h2 className="text-sm font-semibold tracking-wide text-zinc-200 uppercase">
          Mitigation Override Center
        </h2>
        <div className="flex items-center gap-2 text-[10px] font-mono">
          <span className="px-2 py-0.5 rounded bg-zinc-800 text-zinc-400 border border-zinc-700/40">
            {userProfile.role}
          </span>
          <span className="text-zinc-600 truncate max-w-[160px]" title={userProfile.email}>
            {userProfile.email || userProfile.userId}
          </span>
        </div>
      </div>

      <div className="flex items-center gap-4 px-4 py-2 border-b border-zinc-800/40 bg-zinc-900/40 shrink-0">
        <span className="text-[11px] text-zinc-500 font-semibold uppercase tracking-wider">
          Summary
        </span>
        <div className="flex items-center gap-3">
          <span className="flex items-center gap-1.5 text-[12px]">
            <span className="inline-block w-2 h-2 rounded-full bg-amber-400" />
            <span className="text-zinc-300">{stats.active}</span>
            <span className="text-zinc-600">active</span>
          </span>
          <span className="flex items-center gap-1.5 text-[12px]">
            <span className="inline-block w-2 h-2 rounded-full bg-cyan-400" />
            <span className="text-zinc-300">{stats.pending}</span>
            <span className="text-zinc-600">pending</span>
          </span>
          <span className="flex items-center gap-1.5 text-[12px]">
            <span className="inline-block w-2 h-2 rounded-full bg-rose-400" />
            <span className="text-zinc-300">{stats.failed}</span>
            <span className="text-zinc-600">failed</span>
          </span>
          <span className="flex items-center gap-1.5 text-[12px]">
            <span className="inline-block w-2 h-2 rounded-full bg-emerald-500" />
            <span className="text-zinc-300">{stats.resolved}</span>
            <span className="text-zinc-600">resolved</span>
          </span>
          <span className="text-zinc-700">|</span>
          <span className="text-zinc-600 text-[12px]">{stats.total} total</span>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        <ActiveInterventionsTable
          interventions={interventions}
          userProfile={userProfile}
          onExecute={execute}
          pendingMap={pendingMap}
        />
      </div>

      <ToastNotification />
    </div>
  );
}
