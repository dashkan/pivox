import { aipStringLiteral } from '@pivox/ui/resource-admin';

/**
 * Composes the active secret filters into one AIP-160 expression. Secrets expose
 * a single filterable field — `displayName` (substring match, `:`) — so there is
 * no ANDing today; the shape mirrors the connector filter builder so a future
 * facet drops in the same way. Returns undefined when nothing is active.
 */
export function buildSecretFilter(
  filters: Record<string, string>,
): string | undefined {
  const name = filters.displayName?.trim();
  if (name) return `displayName:${aipStringLiteral(name)}`;
  return undefined;
}
