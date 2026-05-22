/**
 * The shared contract for the OAuth broker redirect flow.
 *
 * The broker (`/internal/v1/auth/{provider}/start` → IdP → `/callback`)
 * finishes by redirecting to a `return` URL with the credential material
 * in the URL fragment. Each app implements a `RedirectTransport` that
 * opens the broker flow and recovers that fragment; `parseBrokerRedirect`
 * turns the fragment into a typed result both transports share.
 */

// Single source of truth for the credential kinds — the runtime set and
// the BrokerCredentialKind type are both derived from this tuple.
const CREDENTIAL_KINDS = [
  'github_access_token',
  'google_id_token',
  'oidc_id_token',
] as const;

const CREDENTIAL_KIND_SET: ReadonlySet<string> = new Set(CREDENTIAL_KINDS);

/**
 * Discriminates which Firebase credential the client must build from a
 * broker result: a GitHub access token, a Google id_token, or a generic
 * OIDC id_token.
 */
export type BrokerCredentialKind = (typeof CREDENTIAL_KINDS)[number];

/**
 * A parsed broker redirect. On success it carries the credential
 * material; on failure it carries the IdP/broker error code.
 */
export type BrokerRedirectResult =
  | {
      ok: true;
      provider: string;
      kind: BrokerCredentialKind;
      token: string;
      accessToken?: string;
      nonce?: string;
    }
  | { ok: false; error: string; errorDescription?: string };

/**
 * The platform boundary for broker-driven auth. Implemented per
 * platform — a popup + callback page in the browser, a loopback HTTP
 * server (or custom scheme) in Electron — so feature code stays
 * transport-agnostic.
 *
 * Both methods need the Cloud Controller origin that hosts the broker;
 * each implementation knows its own (same-origin for the web app, an
 * absolute origin resolved over IPC for Electron), so callers never
 * pass a base URL.
 */
export interface RedirectTransport {
  /**
   * Opens the broker OAuth flow for `provider` and resolves with the
   * parsed result.
   */
  runBrokerOAuth(input: {
    provider: string;
    loginHint?: string;
  }): Promise<BrokerRedirectResult>;

  /**
   * Resolves the Firebase OIDC provider id for an email's domain via
   * the broker's `:resolveProvider` endpoint, or `null` when the
   * domain has no enabled SSO. Rejects on network / non-404 errors.
   */
  resolveSsoProvider(email: string): Promise<string | null>;
}

/**
 * Parses the fragment the broker appends to the `return` URL into a
 * typed result. Accepts a bare fragment (`a=b&c=d`), a fragment with a
 * leading `#`, or a full URL containing one. An error fragment always
 * wins over a success fragment so a malformed mix fails closed.
 */
export function parseBrokerRedirect(fragment: string): BrokerRedirectResult {
  const hashIndex = fragment.indexOf('#');
  const raw = hashIndex >= 0 ? fragment.slice(hashIndex + 1) : fragment;
  const params = new URLSearchParams(raw);

  const error = params.get('error');
  if (error) {
    const errorDescription = params.get('error_description');
    return errorDescription
      ? { ok: false, error, errorDescription }
      : { ok: false, error };
  }

  const provider = params.get('provider');
  const kind = params.get('kind');
  const token = params.get('token');
  if (!provider || !kind || !token || !CREDENTIAL_KIND_SET.has(kind)) {
    return { ok: false, error: 'invalid_broker_response' };
  }

  const accessToken = params.get('access_token');
  const nonce = params.get('nonce');
  return {
    ok: true,
    provider,
    kind: kind as BrokerCredentialKind,
    token,
    ...(accessToken ? { accessToken } : {}),
    ...(nonce ? { nonce } : {}),
  };
}

/**
 * Builds the broker's `/start` URL for `provider`. The transport opens
 * it in the system browser (Electron) or a popup (web); `returnUrl` is
 * where the broker redirects back with the credential fragment.
 */
export function buildBrokerStartUrl(input: {
  baseUrl: string;
  provider: string;
  returnUrl: string;
  loginHint?: string;
}): string {
  const origin = input.baseUrl.replace(/\/$/, '');
  const params = new URLSearchParams({ return: input.returnUrl });
  if (input.loginHint) {
    params.set('login_hint', input.loginHint);
  }
  const provider = encodeURIComponent(input.provider);
  return `${origin}/internal/v1/auth/${provider}/start?${params.toString()}`;
}
