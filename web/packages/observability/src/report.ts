/**
 * The single error sink. Every error path — asyncHandler's `.catch`, the
 * global `unhandledrejection` / `error` listeners — funnels through here.
 *
 * Today it logs to the console. This is the one place to wire telemetry
 * (Sentry, Datadog, etc.) later; callers never change.
 */

/**
 * Coerce an arbitrary rejection reason into an Error.
 *
 * A promise can reject with — and `throw` can throw — any value, not just
 * an Error. Non-Error reasons are preserved on `.cause` rather than
 * stringified, so telemetry keeps the original payload without risking an
 * `[object Object]` message.
 */
function toError(reason: unknown): Error {
  if (reason instanceof Error) return reason;
  if (typeof reason === 'string') return new Error(reason);
  return new Error('Non-Error rejection reason', { cause: reason });
}

export interface ErrorContext {
  /** Where the error was intercepted — e.g. 'asyncHandler', 'unhandledrejection'. */
  source?: string;
  [key: string]: unknown;
}

/**
 * Report an error. `reason` may be anything (Error, string, object, …).
 */
export function reportError(reason: unknown, context?: ErrorContext): void {
  const error = toError(reason);
  console.error('[pivox]', error, context ?? {});
  // Telemetry hook lands here, e.g.:
  //   Sentry.captureException(error, { extra: context });
}
