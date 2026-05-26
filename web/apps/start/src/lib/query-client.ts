import { QueryClient } from '@tanstack/react-query';

/**
 * Singleton QueryClient for the start app. Created at module load
 * so SSR + the client share one instance — the SSR pass primes the
 * cache, hydration picks it up.
 *
 * Stays vanilla for now (no global defaults). Per-query options
 * (staleTime, retry, etc.) live on the query call sites; when we
 * find a pattern that's consistent across enough queries to be
 * worth defaulting, lift it here.
 */
export const queryClient = new QueryClient();
