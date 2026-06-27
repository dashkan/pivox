import { useSyncExternalStore } from "react";

/** Hash routing — robust under Keycloak's nested resource path (no server
 * rewrites needed for deep links). */
export type RouteId =
  | "personal-info"
  | "account-security"
  | "device-activity"
  | "applications"
  | "linked-accounts"
  | "groups";

const ROUTES: RouteId[] = [
  "personal-info",
  "account-security",
  "device-activity",
  "applications",
  "linked-accounts",
  "groups",
];

const DEFAULT_ROUTE: RouteId = "personal-info";

function subscribe(callback: () => void): () => void {
  window.addEventListener("hashchange", callback);
  return () => window.removeEventListener("hashchange", callback);
}

function getSnapshot(): RouteId {
  const id = window.location.hash.replace(/^#\/?/, "") as RouteId;
  return ROUTES.includes(id) ? id : DEFAULT_ROUTE;
}

export function useRoute(): RouteId {
  return useSyncExternalStore(subscribe, getSnapshot, () => DEFAULT_ROUTE);
}

export function navigate(route: RouteId): void {
  window.location.hash = `/${route}`;
}
