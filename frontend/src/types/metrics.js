export function createCompoundKey(tenantId, nodeId, pid) {
    return `${tenantId}-${nodeId}-${pid}`;
}
