import {
  AGENT_FILTER_ANY,
  AGENT_FILTER_CLOUD,
  aipStringLiteral,
} from '@pivox/ui/resource-admin';

/**
 * Composes the active connector filters into one AIP-160 expression (predicates
 * ANDed). Display name is a substring match (`:`); agent is an exact match
 * (`=`), where the "Cloud" sentinel matches connectors with no agent (`agent=""`)
 * and "Any" applies no agent predicate. Returns undefined when nothing is active.
 */
export function buildConnectorFilter(
  filters: Record<string, string>,
): string | undefined {
  const predicates: string[] = [];

  const name = filters.displayName?.trim();
  if (name) predicates.push(`displayName:${aipStringLiteral(name)}`);

  const agent = filters.agent;
  if (agent && agent !== AGENT_FILTER_ANY) {
    const value = agent === AGENT_FILTER_CLOUD ? '' : agent;
    predicates.push(`agent=${aipStringLiteral(value)}`);
  }

  return predicates.length > 0 ? predicates.join(' AND ') : undefined;
}
