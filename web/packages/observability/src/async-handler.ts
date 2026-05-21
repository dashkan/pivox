import { reportError } from './report';

/**
 * Wrap an async event handler so it satisfies the `() => void` shape that
 * DOM / React event props expect, and so any rejection is routed to
 * `reportError` instead of becoming an unhandled rejection.
 *
 * Usage:
 *   onClick={asyncHandler(async () => { await doThing(); })}
 *
 * Note this is a *safety net*, not a substitute for in-handler error UX.
 * A handler with a user-facing failure mode should still try/catch
 * internally and route to UI state — `reportError` only makes the
 * failure observable, it doesn't tell the user anything.
 */
export function asyncHandler<A extends unknown[]>(
  fn: (...args: A) => Promise<unknown>,
): (...args: A) => void {
  return (...args: A) => {
    // The `.catch` settles the chain, so the trailing promise never
    // rejects — `void` it to satisfy no-floating-promises.
    void fn(...args).catch((error: unknown) => {
      reportError(error, { source: 'asyncHandler' });
    });
  };
}
