/**
 * Reduce an attacker-controllable `from` value (it rides in the URL) to a safe,
 * same-app path. Returns `pathname + search + hash` when `from` resolves to the
 * app's own origin, or `null` to force the caller's list-route fallback. This is
 * an open-redirect defense — see the design doc's "Security — sanitize `from`".
 *
 * FormPage never calls this; the ROUTE does, then injects the resulting
 * `cancel` / `onSubmitSuccess` handlers into the provider. Kept in the generic
 * package (not a route file) so start and electron share one hardened
 * implementation and one test.
 *
 * Rejects: external URLs (`https://evil.com/…`), protocol-relative
 * (`//evil.com`), backslash-normalization tricks (`/\evil.com`, `/\/evil.com`),
 * and any non-`/`-leading value. Accepts: a single-slash-leading path that
 * resolves same-origin. Because only `pathname + search + hash` is ever
 * returned, an accepted value can't carry a foreign origin even if `new URL`
 * were lenient.
 *
 * @param appOrigin The app's own origin (start: `window.location.origin`;
 *   electron: its `pivox://` scheme host or file origin).
 */
export function safeInternalPath(
  from: string | undefined | null,
  appOrigin: string,
): string | null {
  if (!from) return null;
  // Reject scheme-relative ("//evil.com"), backslash tricks ("/\evil.com"),
  // and anything not starting with a single "/". The backslash checks matter
  // because browsers normalize "\" to "/" in URLs, so "/\evil.com" would
  // otherwise resolve as "//evil.com". The encoded-slash checks are
  // defense-in-depth (lowercased so "%2F"/"%5C" are caught too) — the actual
  // boundary is the same-origin check + path-only return below, which rejects a
  // foreign origin regardless of encoding.
  const lower = from.toLowerCase();
  if (
    !from.startsWith('/') ||
    from.startsWith('//') ||
    from.startsWith('/\\') ||
    lower.startsWith('/%2f') ||
    lower.startsWith('/%5c')
  ) {
    return null;
  }
  try {
    const url = new URL(from, appOrigin); // resolve against our own origin
    if (url.origin !== appOrigin) return null; // absolute URL to another host → reject
    return url.pathname + url.search + url.hash; // strip origin; keep only the path
  } catch {
    return null;
  }
}
