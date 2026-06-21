import { create } from "zustand";
import { MAX_TOASTS, TOAST_DURATION_MS } from "../types/mitigation";
let toastSeq = 0;
export const useMitigationStore = create((set, get) => ({
    interventions: [],
    toasts: [],
    pushIntervention: (node) => {
        set((state) => {
            const existing = state.interventions.findIndex((i) => i.mitigationId === node.mitigationId);
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
            interventions: state.interventions.map((i) => i.mitigationId === id ? { ...i, currentStatus: status } : i),
        }));
    },
    removeIntervention: (id) => {
        set((state) => ({
            interventions: state.interventions.filter((i) => i.mitigationId !== id),
        }));
    },
    setInterventions: (nodes) => set({ interventions: nodes }),
    pushToast: (msg) => {
        const id = `toast_${++toastSeq}`;
        const toast = { ...msg, id, timestamp: Date.now() };
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
}));
