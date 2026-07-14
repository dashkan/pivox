/**
 * Runtime environment for the account console. In production Keycloak's
 * index.ftl injects `<script id="environment" type="application/json">…` with
 * the realm/client/URLs; we parse that. In dev we fall back to index.html's
 * inline block (pointing at the local test container).
 */
export type Environment = {
  authServerUrl?: string;
  authUrl?: string;
  serverBaseUrl?: string;
  realm: string;
  clientId: string;
  resourceUrl?: string;
  baseUrl?: string;
  referrerName?: string;
  referrerUrl?: string;
  features?: Features;
  locale?: string;
};

export type Features = {
  isLinkedAccountsEnabled?: boolean;
  isViewGroupsEnabled?: boolean;
  isViewOrganizationsEnabled?: boolean;
  deleteAccountAllowed?: boolean;
  updateEmailFeatureEnabled?: boolean;
};

function readEnvironment(): Environment {
  const el = document.getElementById("environment");
  if (el?.textContent) {
    try {
      // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- Keycloak's index.ftl injects the environment as JSON in the `#environment` <script> tag; JSON.parse returns `any` at this DOM boundary and is guarded by the surrounding try/catch.
      return JSON.parse(el.textContent) as Environment;
    } catch {
      // fall through to defaults
    }
  }
  return {
    authServerUrl: "http://localhost:8089",
    serverBaseUrl: "http://localhost:8089",
    realm: "pivoxdev",
    clientId: "account-console",
  };
}

export const environment = readEnvironment();

/**
 * Strip trailing slashes so a base URL is always safe to concatenate with a
 * leading-slash path. Keycloak is NOT consistent here: `authServerUrl` arrives
 * with a trailing slash while `serverBaseUrl` does not, so `${base}/resources/…`
 * yields `https://host//resources/…`. The ingress rejects the double slash
 * outright (connection error, not a 404), which silently broke the i18next
 * message-bundle load and rendered every label as its raw key.
 */
function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, "");
}

/** Base URL of the Keycloak server (for keycloak-js). */
export const authServerUrl = trimTrailingSlash(
  environment.authServerUrl ??
    environment.authUrl ??
    environment.serverBaseUrl ??
    "",
);

/** Base URL of the Account REST API for this realm. */
export const accountApiBase = `${trimTrailingSlash(
  environment.serverBaseUrl ?? authServerUrl,
)}/realms/${environment.realm}/account`;

export const features: Features = environment.features ?? {};
