import type { UserRole } from "../types/mitigation";
import { ROLE_HIERARCHY } from "../types/mitigation";



interface RBACGuardProps {
  requiredRole: UserRole;
  currentRole: UserRole;
  children: React.ReactNode;
  tooltip?: string;
}

export function RBACGuard({
  requiredRole,
  currentRole,
  children,
  tooltip,
}: RBACGuardProps) {
  const hasAccess = ROLE_HIERARCHY[currentRole] >= ROLE_HIERARCHY[requiredRole];

  if (hasAccess) {
    return <>{children}</>;
  }

  const tip =
    tooltip ??
    `Requires ${requiredRole} role (current: ${currentRole})`;

  return (
    <span className="group relative inline-block">
      <span
        className="inline-flex items-center opacity-40 grayscale pointer-events-none cursor-not-allowed"
        aria-disabled="true"
      >
        {children}
      </span>
      <span className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 px-2.5 py-1 rounded bg-zinc-800 text-zinc-300 text-[10px] font-mono whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none z-50 shadow-lg border border-zinc-700/50">
        {tip}
        <span className="absolute top-full left-1/2 -translate-x-1/2 w-0 h-0 border-l-4 border-r-4 border-t-4 border-transparent border-t-zinc-800" />
      </span>
    </span>
  );
}
