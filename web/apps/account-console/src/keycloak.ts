import Keycloak from "keycloak-js";

import { authServerUrl, environment } from "./env";

/**
 * keycloak-js client for the account console. Uses the built-in
 * `account-console` public client (provided by the realm), so the token has the
 * right audience for the Account REST API.
 */
export const keycloak = new Keycloak({
  url: authServerUrl,
  realm: environment.realm,
  clientId: environment.clientId,
});

export async function initAuth(): Promise<void> {
  await keycloak.init({
    onLoad: "login-required",
    pkceMethod: "S256",
    responseMode: "query",
    // The login iframe is unreliable behind dev proxies / third-party-cookie
    // restrictions; updateToken() covers refresh instead.
    checkLoginIframe: false,
  });
}

/** A fresh access token, refreshing if it expires within 30s. */
export async function freshToken(): Promise<string> {
  try {
    await keycloak.updateToken(30);
  } catch {
    await keycloak.login();
  }
  return keycloak.token ?? "";
}

export function logout(): void {
  // Post-logout must be a redirect URI the account-console client allows
  // (scoped to /realms/{realm}/account/*). The bare origin isn't, so use the
  // account console's own base URL — KC injects it as baseUrl. After logout
  // it lands here, sees no session, and shows the login screen.
  const redirectUri =
    environment.baseUrl ||
    `${authServerUrl}/realms/${environment.realm}/account/`;
  void keycloak.logout({ redirectUri });
}

/** Run an application-initiated action (e.g. UPDATE_PASSWORD, CONFIGURE_TOTP)
 * via the login flow — renders the matching Pivox-themed action page, then
 * returns to the account console. */
export function runAction(action: string): void {
  void keycloak.login({ action });
}
