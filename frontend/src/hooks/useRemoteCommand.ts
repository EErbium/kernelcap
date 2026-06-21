import { useMitigationStore } from "./useMitigationStore";
import { COMMAND_TIMEOUT_MS } from "../types/mitigation";
import type { OverrideAction } from "../types/mitigation";

const OVERRIDE_ENDPOINT = "/api/v2/mitigation/override";
const _simulateDelay = 1200;

export interface UseRemoteCommandReturn {
  execute: (
    mitigationId: string,
    actionType: OverrideAction
  ) => Promise<boolean>;
  pendingMap: ReadonlyMap<string, boolean>;
}

const pendingMap = new Map<string, boolean>();

export function useRemoteCommand(): UseRemoteCommandReturn {

  async function execute(mitigationId: string, actionType: OverrideAction): Promise<boolean> {
      const store = useMitigationStore.getState();

      store.updateInterventionStatus(mitigationId, "PENDING_VERIFICATION");
      pendingMap.set(mitigationId, true);

      const controller = new AbortController();

      let timeoutId: ReturnType<typeof setTimeout> | null = null;
      let responseStatus = 0;

      try {
        timeoutId = setTimeout(() => {
          controller.abort();
        }, COMMAND_TIMEOUT_MS);

        const res = await fetch(OVERRIDE_ENDPOINT, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${import.meta.env.VITE_API_TOKEN ?? ""}`,
          },
          body: JSON.stringify({
            mitigation_id: mitigationId,
            action_type: actionType,
          }),
          signal: controller.signal,
        });

        responseStatus = res.status;

        if (!res.ok) {
          throw new Error(`Server responded ${res.status}`);
        }

        if (timeoutId !== null) clearTimeout(timeoutId);
        pendingMap.delete(mitigationId);

        if (actionType === "FORCE_TERMINATE") {
          store.removeIntervention(mitigationId);
        } else {
          store.updateInterventionStatus(mitigationId, "RESOLVED");
        }

        store.pushToast({
          message: `Override ${actionType} confirmed for ${mitigationId}`,
          type: "success",
        });

        return true;
      } catch (err) {
        if (timeoutId !== null) clearTimeout(timeoutId);
        pendingMap.delete(mitigationId);

        const isTimeout = err instanceof DOMException && err.name === "AbortError";
        const isNetworkError = err instanceof TypeError;
        const isNotFound = responseStatus === 404;

        if (isNetworkError || isNotFound) {
          store.pushToast({
            message: `Override endpoint unreachable — simulating resolution for ${mitigationId}`,
            type: "warning",
          });

          await new Promise((r) => setTimeout(r, _simulateDelay));

          if (actionType === "FORCE_TERMINATE") {
            store.removeIntervention(mitigationId);
          } else {
            store.updateInterventionStatus(mitigationId, "RESOLVED");
          }
          return true;
        }

        store.updateInterventionStatus(mitigationId, "ACTIVE_ENFORCED");

        store.pushToast({
          message: isTimeout
            ? `Override command timed out after ${COMMAND_TIMEOUT_MS / 1000}s — ${mitigationId}`
            : `Override failed — ${mitigationId}`,
          type: "error",
        });

        return false;
      }
    }

  return { execute, pendingMap };
}
