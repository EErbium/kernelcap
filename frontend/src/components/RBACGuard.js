import { Fragment as _Fragment, jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { ROLE_HIERARCHY } from "../types/mitigation";
export function RBACGuard({ requiredRole, currentRole, children, tooltip, }) {
    const hasAccess = ROLE_HIERARCHY[currentRole] >= ROLE_HIERARCHY[requiredRole];
    if (hasAccess) {
        return _jsx(_Fragment, { children: children });
    }
    const tip = tooltip ??
        `Requires ${requiredRole} role (current: ${currentRole})`;
    return (_jsxs("span", { className: "group relative inline-block", children: [_jsx("span", { className: "inline-flex items-center opacity-40 grayscale pointer-events-none cursor-not-allowed", "aria-disabled": "true", children: children }), _jsxs("span", { className: "absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 px-2.5 py-1 rounded bg-zinc-800 text-zinc-300 text-[10px] font-mono whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none z-50 shadow-lg border border-zinc-700/50", children: [tip, _jsx("span", { className: "absolute top-full left-1/2 -translate-x-1/2 w-0 h-0 border-l-4 border-r-4 border-t-4 border-transparent border-t-zinc-800" })] })] }));
}
