/**
 * Resolves the SSO provider for an email's domain via the broker's
 * `:resolveProvider` endpoint — the first step of the email-first SSO
 * sign-in flow.
 *
 * Returns the Firebase OIDC provider id (e.g. `oidc.acme`) when the
 * domain has a verified, enabled SsoConfig, or `null` when it does
 * not. The broker collapses {domain unknown, domain unverified,
 * SsoConfig disabled} into a single 200 response with an empty body
 * (anti-enumeration via response-shape uniformity), so `null` here
 * simply means "no SSO for this domain — fall back to password".
 *
 * Any error response (non-200) — including 404, which previously
 * carried the "no SSO" semantics but now signals a genuine missing
 * endpoint — rejects; the caller decides retry/fallback.
 *
 * `baseUrl` is the Cloud Controller origin: same-origin (often empty)
 * for the web app, the absolute origin for Electron.
 */
export async function resolveSsoProvider(
  email: string,
  baseUrl: string,
): Promise<string | null> {
  const origin = baseUrl.replace(/\/$/, '');
  const response = await fetch(`${origin}/internal/v1/auth:resolveProvider`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ email }),
  });

  if (!response.ok) {
    throw new Error(`resolveProvider failed with status ${response.status}`);
  }

  const data = (await response.json()) as { provider_id?: string };
  return data.provider_id ?? null;
}
