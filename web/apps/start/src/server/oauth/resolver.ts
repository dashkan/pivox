import { staticProviders, type ProviderConfig } from './providers'

/**
 * Resolves a provider id ("github" / "google" / future "sso-<orgId>")
 * into the `ProviderConfig` the start/callback routes need. Callers
 * should never import `staticProviders` directly — this indirection
 * is what lets us add enterprise SSO later without touching the
 * route handlers.
 *
 * Today: static lookup only. Missing / misconfigured env vars mean
 * the provider's `clientId` or `clientSecret` is an empty string; we
 * surface that as a 500-level config error at call time rather than
 * silently build a broken authorize URL.
 *
 * Future (SSO): the `sso-*` branch below will query the DB for a
 * tenant's configured OIDC/SAML identity provider and return a
 * dynamically-constructed `ProviderConfig`. The routes stay exactly
 * as they are — all provider heterogeneity lives inside here and
 * inside `providers.ts::toFirebaseCredential`.
 */
export async function resolveProvider(name: string): Promise<ProviderConfig | null> {
  // 1. Static providers from env (GH, Google, Apple once implemented).
  const staticCfg = staticProviders[name]
  if (staticCfg) {
    if (!staticCfg.clientId || !staticCfg.clientSecret) {
      // Helpful error in dev when someone added a provider key but
      // forgot to populate the env vars in .envrc / .envrc.<mode>.
      throw new Error(
        `OAuth provider "${name}" is in the registry but its credentials are missing. ` +
          `Set the corresponding CLIENT_ID / CLIENT_SECRET env vars.`
      )
    }
    return staticCfg
  }

  // 2. Dynamic SSO providers (per-org OIDC/SAML). Not implemented
  //    yet — when we add it, it'll look roughly like:
  //
  //      if (name.startsWith('sso-')) {
  //        const orgId = name.slice(4)
  //        const row = await db.ssoProviders.findByOrg(orgId)
  //        if (!row) return null
  //        return {
  //          id: name,
  //          authorizeUrl: row.authorize_url,
  //          tokenUrl: row.token_url,
  //          scopes: row.scopes.split(' '),
  //          clientId: row.client_id,
  //          clientSecret: row.client_secret,  // encrypted at rest, decrypted here
  //          toFirebaseCredential: oidcIdTokenAdapter(name),
  //        }
  //      }
  //
  //    The routes stay unchanged. Only this resolver grows.
  if (name.startsWith('sso-')) {
    throw new Error('Enterprise SSO is not implemented yet.')
  }

  return null
}
