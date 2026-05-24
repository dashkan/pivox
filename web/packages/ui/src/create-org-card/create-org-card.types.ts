export interface CreateOrgState {
  /** Human-readable name shown in the UI. */
  displayName: string;
  /**
   * Permanent URL-safe slug. ^[a-z][a-z0-9-]{3,19}$ (mirrors the
   * server's buf-validate rule).
   */
  organizationId: string;
  error: string | null;
}

export interface CreateOrgActions {
  updateDisplayName: (value: string) => void;
  updateOrganizationId: (value: string) => void;
  formAction: (payload: FormData) => void;
  /**
   * Footer "Wrong account?" sign-out link. The card knows nothing
   * about auth — the consumer wires this to its auth provider.
   */
  signOut: () => void;
}

export interface CreateOrgMeta {
  displayNameRef: React.RefObject<HTMLInputElement | null>;
  /**
   * True once the user types in the slug field — until then, the
   * slug auto-derives from `displayName` (Slack workspaces / Notion
   * teams style). The card writes this flag through
   * `updateOrganizationId`; the feature decides when to honor it.
   */
  slugTouched: boolean;
}

export interface CreateOrgContextValue {
  state: CreateOrgState;
  actions: CreateOrgActions;
  meta: CreateOrgMeta;
}
