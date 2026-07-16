/**
 * React-query key for the connectors page's composite agent-options query. The
 * loader (SSR) primes this key and the route's client `useQuery` reads it — a
 * single shared builder so the two can never drift.
 */
export function connectorAgentsQueryKey(orgSlug: string) {
  return ['connector-agents', orgSlug] as const;
}
