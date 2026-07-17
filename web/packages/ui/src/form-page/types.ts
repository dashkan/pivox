/** Create vs edit — injected as data, never a component boolean prop. */
export type FormMode = 'create' | 'edit';

/**
 * Generic, controlled form-page state — the read half of the DI interface.
 * Fully presentational: no async, no router, and crucially NO form values.
 * Form values, `patch`, and per-field validity are resource-specific and live
 * in a second, resource-owned context (see the design doc's "Where the form
 * values live"), exactly as domain rows never enter `GridState`.
 */
export interface FormPageState<T> {
  /**
   * Data (like `GridState.isLoading`), not a component prop. Lets shared copy
   * read "create" vs "edit"; nothing forks its render tree on it — create/edit
   * divergence is handled by explicit variant field-sets
   * (`patterns-explicit-variants`).
   */
  mode: FormMode;
  /** A create/update write is in flight. Gates Cancel + drives the "Saving…" label. */
  pending: boolean;
  /** A failed-submit message to surface inline in `Actions`, or null. */
  error: string | null;
  /** The resource form is valid and may be submitted. Derived in the provider (5.1). */
  canSubmit: boolean;
  /** The form has unsaved edits. Derived in the provider; drives the navigate-away guard. */
  dirty: boolean;
  /**
   * Edit-mode record load. In create mode all three are inert (record null,
   * recordLoading false, loadError null). Injected by the route: SSR-prefetched
   * (start) or client-fetched (electron).
   */
  record: T | null;
  recordLoading: boolean;
  loadError: string | null;
}

/** The write half of the DI interface — every FormPage mutation flows through here. */
export interface FormPageActions {
  /** Commit the form. Takes no args — the provider closed over the resource values. */
  submit: () => void;
  /** Abandon; navigate to the launching route (sanitized `from`, else the list). */
  cancel: () => void;
  /**
   * Edit-only delete. `undefined` in create — and because create's variant never
   * composes `FormPage.Delete`, the button is absent, not merely disabled.
   */
  delete?: () => void;
}

/** Metadata the parts can't derive from state alone. */
export interface FormPageMeta {
  /** Human resource label ("connector") for titles + confirm copy. */
  resourceLabel: string;
  /**
   * Optional dirty-signal sink so a router-specific navigation blocker (start's
   * `useBlocker`, electron's own) can live in the route while FormPage stays
   * router-free. See the dirty-guard section of the design.
   */
  onDirtyChange?: (dirty: boolean) => void;
}

/**
 * The dependency-injected form-page interface (state + actions + meta). Any
 * provider that implements this drives the same `FormPage.*` UI — the connectors
 * form provider today, a flat-resource or electron-local provider tomorrow.
 */
export interface FormPageContextValue<T> {
  state: FormPageState<T>;
  actions: FormPageActions;
  meta: FormPageMeta;
}
