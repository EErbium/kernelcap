import type { UserProfile, UserRole } from "../types/mitigation";

const VALID_ROLES: UserRole[] = ["Viewer", "Operator", "Admin"];


function isUserRole(v: string): v is UserRole {
  return VALID_ROLES.includes(v as UserRole);
}

export function parseJwtPayload(token: string): Record<string, unknown> | null {
  try {
    const parts = token.split(".");
    if (parts.length !== 3) return null;
    const payload = parts[1];
    const normalized = payload.replace(/-/g, "+").replace(/_/g, "/");
    const decoded = atob(normalized);
    return JSON.parse(decoded);
  } catch {
    return null;
  }
}

export function extractUserProfile(token: string): UserProfile | null {
  const claims = parseJwtPayload(token);
  if (!claims) return null;
  return {
    userId: String(claims.sub ?? claims.userId ?? claims.user_id ?? ""),
    email: String(claims.email ?? ""),
    role: isUserRole(String(claims.role ?? ""))
      ? (String(claims.role) as UserRole)
      : "Viewer",
  };
}

export function getStoredToken(): string {
  return import.meta.env.VITE_API_TOKEN ?? "";
}

export function getStoredUserProfile(): UserProfile {
  const token = getStoredToken();
  if (!token) {
    return {
      userId: "dev-user",
      email: "dev@local",
      role: import.meta.env.VITE_USER_ROLE === "Admin"
        ? "Admin"
        : import.meta.env.VITE_USER_ROLE === "Operator"
        ? "Operator"
        : "Viewer",
    };
  }
  return extractUserProfile(token) ?? {
    userId: "unknown",
    email: "",
    role: "Viewer",
  };
}
