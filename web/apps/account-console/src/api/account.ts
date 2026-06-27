import { accountApiBase } from "../env";
import { freshToken } from "../keycloak";

/* Minimal representations — only the fields the UI uses. */
export type UserRepresentation = {
  id?: string;
  username?: string;
  email?: string;
  firstName?: string;
  lastName?: string;
  emailVerified?: boolean;
  userProfileMetadata?: unknown;
};

export type DeviceRepresentation = {
  id?: string;
  device?: string;
  os?: string;
  osVersion?: string;
  browser?: string;
  current?: boolean;
  ipAddress?: string;
  lastAccess?: number;
  mobile?: boolean;
  sessions?: SessionRepresentation[];
};

export type SessionRepresentation = {
  id: string;
  ipAddress?: string;
  started?: number;
  lastAccess?: number;
  expires?: number;
  browser?: string;
  current?: boolean;
  clients?: { clientId: string; clientName?: string }[];
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = await freshToken();
  const res = await fetch(accountApiBase + path, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/json",
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  if (!res.ok) {
    throw new Error(`${res.status} ${res.statusText}`);
  }
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const getPersonalInfo = () =>
  request<UserRepresentation>("/?userProfileMetadata=true");

export const savePersonalInfo = (data: UserRepresentation) =>
  request<void>("/", { method: "POST", body: JSON.stringify(data) });

export const getDevices = () =>
  request<DeviceRepresentation[]>("/sessions/devices");

export const deleteSession = (id?: string) =>
  request<void>(id ? `/sessions/${id}` : "/sessions", { method: "DELETE" });

/* ---- Credentials (account security) ---- */
export type CredentialMetadata = {
  credential: {
    id: string;
    type: string;
    userLabel?: string;
    createdDate?: number;
  };
};
export type CredentialContainer = {
  type: string;
  category: "basic-authentication" | "two-factor" | "passwordless" | string;
  displayName?: string;
  helptext?: string;
  createAction?: string;
  updateAction?: string;
  removeable: boolean;
  userCredentialMetadatas: CredentialMetadata[];
};

export const getCredentials = () =>
  request<CredentialContainer[]>("/credentials");

export const deleteCredential = (id: string) =>
  request<void>(`/credentials/${id}`, { method: "DELETE" });

/* ---- Applications ---- */
export type Application = {
  clientId: string;
  clientName?: string;
  description?: string;
  userConsentRequired: boolean;
  inUse: boolean;
  offlineAccess: boolean;
  effectiveUrl?: string;
  consent?: {
    grantedScopes: { id: string; name: string; displayTest?: string }[];
    createdDate?: number;
    lastUpdatedDate?: number;
  };
};

export const getApplications = () => request<Application[]>("/applications");

export const deleteConsent = (clientId: string) =>
  request<void>(`/applications/${encodeURIComponent(clientId)}/consent`, {
    method: "DELETE",
  });

/* ---- Linked (identity provider) accounts ---- */
export type LinkedAccount = {
  connected: boolean;
  providerAlias: string;
  providerName: string;
  displayName?: string;
  social: boolean;
  linkedUsername?: string;
};

export const getLinkedAccounts = () =>
  request<LinkedAccount[]>("/linked-accounts");

export const linkAccount = (alias: string) => {
  const redirectUri = window.location.href;
  return request<{ accountLinkUri: string }>(
    `/linked-accounts/${encodeURIComponent(alias)}?providerId=${encodeURIComponent(alias)}&redirectUri=${encodeURIComponent(redirectUri)}`,
  );
};

export const unLinkAccount = (alias: string) =>
  request<void>(`/linked-accounts/${encodeURIComponent(alias)}`, {
    method: "DELETE",
  });

/* ---- Groups ---- */
export type Group = { id?: string; name?: string; path?: string };

export const getGroups = () => request<Group[]>("/groups");

/* Keycloak returns client names as i18n keys like `${client_account-console}`
 * (its own UI resolves them via a message bundle we don't load). Prettify the
 * common `${client_*}` keys to a Title-Cased label; fall back for other keys. */
export function resolveLabel(name: string | undefined, fallback: string): string {
  if (!name) return fallback;
  const clientKey = /^\$\{client_(.+)\}$/.exec(name);
  if (clientKey) {
    return clientKey[1]
      .replace(/[-_]/g, " ")
      .replace(/\b\w/g, (c) => c.toUpperCase());
  }
  return /^\$\{.*\}$/.test(name) ? fallback : name;
}
