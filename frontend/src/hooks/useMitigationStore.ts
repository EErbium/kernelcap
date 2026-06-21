import { create } from "zustand";
import type {
  ActiveInterventionNode,
  MitigationStatus,
  ToastMessage,
} from "../types/mitigation";
import { MAX_TOASTS, TOAST_DURATION_MS } from "../types/mitigation";

export interface MitigationStoreState {
  interventions: ActiveInterventionNode[];
  toasts: ToastMessage[];

  pushIntervention: (node: ActiveInterventionNode) => void;
  updateInterventionStatus: (id: string, status: MitigationStatus) => void;
  removeIntervention: (id: string) => void;
  setInterventions: (nodes: ActiveInterventionNode[]) => void;

  pushToast: (msg: Omit<ToastMessage, "id" | "timestamp">) => string;
  dismissToast: (id: string) => void;
}

let toastSeq = 0;


export const useMitigationStore = create<MitigationStoreState>(
  (set, get) => ({
    interventions: [],
    toasts: [],

    pushIntervention: (node) => {
      set((state) => {
        const existing = state.interventions.findIndex(
          (i) => i.mitigationId === node.mitigationId
        );
        if (existing >= 0) {
          const next = [...state.interventions];
          next[existing] = node;
          return { interventions: next };
        }
        return { interventions: [node, ...state.interventions] };
      });
    },

    updateInterventionStatus: (id, status) => {
      set((state) => ({
        interventions: state.interventions.map((i) =>
          i.mitigationId === id ? { ...i, currentStatus: status } : i
        ),
      }));
    },

    removeIntervention: (id) => {
      set((state) => ({
        interventions: state.interventions.filter(
          (i) => i.mitigationId !== id
        ),
      }));
    },

    setInterventions: (nodes) => set({ interventions: nodes }),

    pushToast: (msg) => {
      const id = `toast_${++toastSeq}`;
      const toast: ToastMessage = { ...msg, id, timestamp: Date.now() };
      set((state) => ({
        toasts: [toast, ...state.toasts].slice(0, MAX_TOASTS),
      }));
      setTimeout(() => {
        const current = get().toasts;
        if (current.some((t) => t.id === id)) {
          get().dismissToast(id);
        }
      }, TOAST_DURATION_MS);
      return id;
    },

    dismissToast: (id) => {
      set((state) => ({
        toasts: state.toasts.filter((t) => t.id !== id),
      }));
    },
  })
);
