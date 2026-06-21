export type UserRole = "Viewer" | "Operator" | "Admin";

export type MitigationAction =
  | "SIGSTOP_FREEZE"
  | "CONTAINER_PAUSE"
  | "API_REROUTE";

export type MitigationStatus =
  | "ACTIVE_ENFORCED"
  | "PENDING_VERIFICATION"
  | "ROLLBACK_FAILED"
  | "RESOLVED";

export type OverrideAction = "FORCE_RESUME" | "FORCE_TERMINATE";

export interface ActiveInterventionNode {
  mitigationId: string;
  nodeId: string;
  targetPid: number;
  processName: string;
  appliedAction: MitigationAction;
  executionTimestamp: number;
  currentStatus: MitigationStatus;
  policyViolationReason: string;
}

export interface UserProfile {
  userId: string;
  email: string;
  role: UserRole;
}

export interface ToastMessage {
  id: string;
  message: string;
  type: "success" | "error" | "warning" | "info";
  timestamp: number;
}



export const ROLE_HIERARCHY: Record<UserRole, number> = {
  Viewer: 0,
  Operator: 1,
  Admin: 2,
};

export const COMMAND_TIMEOUT_MS = 5_000;
export const TOAST_DURATION_MS = 5_000;
export const MAX_TOASTS = 10;
