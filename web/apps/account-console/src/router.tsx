import { createBrowserRouter, type RouteObject } from "react-router-dom";

import { App } from "./app";
import { environment, features } from "./env";
import { AccountSecurity } from "./pages/account-security";
import { Applications } from "./pages/applications";
import { DeviceActivity } from "./pages/device-activity";
import { Groups } from "./pages/groups";
import { LinkedAccounts } from "./pages/linked-accounts";
import { PersonalInfo } from "./pages/personal-info";

/**
 * Path-based routes that mirror Keycloak's own account console exactly, so a
 * bookmark like `…/account/account-security/signing-in` works no matter which
 * account theme is active. Mounted under KC's account base path (the pathname
 * of `environment.baseUrl`, e.g. `/realms/pivox/account`) — KC serves index.ftl
 * for every `/account/*` deep path, so no server rewrites are needed.
 *
 * Route paths are taken verbatim from account-ui's routes.tsx; we register only
 * the pages we implement, feature-gated the same way.
 */
function accountBasename(): string {
  try {
    // baseUrl ends with a trailing slash (…/account/), but react-router needs
    // the basename WITHOUT it — otherwise the callback/base URL `…/account`
    // (no trailing slash) fails the prefix match and the router renders nothing.
    return new URL(environment.baseUrl ?? "").pathname.replace(/\/+$/, "") || "/";
  } catch {
    return "/";
  }
}

const children: RouteObject[] = [
  { index: true, element: <PersonalInfo /> },
  { path: "account-security/signing-in", element: <AccountSecurity /> },
  { path: "account-security/device-activity", element: <DeviceActivity /> },
  ...(features.isLinkedAccountsEnabled
    ? [{ path: "account-security/linked-accounts", element: <LinkedAccounts /> }]
    : []),
  { path: "applications", element: <Applications /> },
  ...(features.isViewGroupsEnabled
    ? [{ path: "groups", element: <Groups /> }]
    : []),
];

export const router = createBrowserRouter(
  [{ path: "/", element: <App />, children }],
  { basename: accountBasename() },
);
