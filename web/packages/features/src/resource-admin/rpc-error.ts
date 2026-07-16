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
