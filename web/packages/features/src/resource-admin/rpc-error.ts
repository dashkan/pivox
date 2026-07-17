import type { components } from '@pivox/client/types';

type RpcStatus = components['schemas']['rpcStatus'];

/** `google.rpc.Code.FAILED_PRECONDITION`. */
const FAILED_PRECONDITION = 9;

/** openapi-fetch surfaces the gateway's `rpcStatus` body as the error union arm. */
export type ApiError = RpcStatus | undefined;

export function isFailedPrecondition(error: ApiError): boolean {
  return error?.code === FAILED_PRECONDITION;
}

/** The server message if present, else `fallback`. */
export function describeRpcError(error: ApiError, fallback: string): string {
  const message = error?.message?.trim();
  return message ? message : fallback;
}

/**
 * Error text for a THROWN failure (a rejected promise), as opposed to the
 * openapi-fetch `{ error }` result arm. openapi-fetch resolves `{ error }` for
 * an HTTP error status, but a transport-level failure — network down, DNS,
 * TLS, or an aborted/cancelled request — REJECTS the promise instead. Those
 * rejections carry no server `rpcStatus`, only a JS `Error` whose message
 * ("Failed to fetch", "The user aborted a request") is not user-facing, so we
 * surface `fallback`. The point is that a caught throw must still produce a
 * visible, readable message — never a silent failure with a stuck spinner.
 */
export function describeCaughtError(_error: unknown, fallback: string): string {
  return fallback;
}

/**
 * Delete-path error text. A `FAILED_PRECONDITION` from a resource with
 * references (a secret still bound to a connector) carries a message that
 * already names the referrers — surface it verbatim rather than a generic
 * failure string.
 */
export function mapDeleteError(error: ApiError): string {
  if (isFailedPrecondition(error)) {
    return describeRpcError(
      error,
      'Still in use — remove the references before deleting.',
    );
  }
  return describeRpcError(error, "Couldn't delete. Please try again.");
}
