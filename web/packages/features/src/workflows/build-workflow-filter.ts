import { aipStringLiteral } from '@pivox/ui/resource-admin';

/**
 * Composes the active workflow filters into one AIP-160 expression. Workflows
 * expose a single filterable field in the list UI — `displayName` (substring
 * match, `:`) — matching the connector/secret filter builders, so a future facet
 * drops in the same way. The server's WorkflowFilter whitelist accepts more
 * fields (description, enabled, timestamps, annotations), but the list surfaces
 * only the name filter today. Returns undefined when nothing is active.
 */
export function buildWorkflowFilter(
  filters: Record<string, string>,
): string | undefined {
  const name = filters.displayName?.trim();
  if (name) return `displayName:${aipStringLiteral(name)}`;
  return undefined;
}
