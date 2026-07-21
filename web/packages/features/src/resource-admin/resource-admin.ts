import type { FormDescriptor } from './form-descriptor';
import type { ListDescriptor } from './list-descriptor';
import type { ComponentType } from 'react';

/**
 * Props a custom create/edit VIEW override receives from the route. The default
 * create/edit is form-based (the `form` descriptor); an override replaces that
 * default with a bespoke React tree — workflows supplies its React Flow canvas
 * here instead of a form spec, keeping the shared List. Connectors uses the form
 * default, so this plumbing is present but unused by it (validated by the parent
 * when workflows lands).
 */
export interface ResourceViewProps {
  /** Org resource name (`organizations/{slug}`). */
  parent: string;
  /** Record leaf id (edit only). */
  id?: string;
  /** Space slug for a space-scoped record; absent = org-direct. */
  space?: string;
  /** Navigate to the launching route (cancel + success). */
  onDone: () => void;
}

/**
 * A full admin resource, descriptor-driven end to end: the always-shared List
 * plus a create/edit surface that is FORM-BASED BY DEFAULT (`form`) and
 * OVERRIDABLE with a custom view (`createView`/`editView`). A new admin resource
 * = one of these + a thin per-app route. The Grid row action defaults to the
 * form edit route; an override just repoints it.
 *
 * @typeParam Row      — a list/detail row.
 * @typeParam Values   — the create/edit form values.
 * @typeParam Extras   — resource-specific list-view data (see `ListDescriptor`).
 * @typeParam Injected — the route-owned slice of `Extras`.
 * @typeParam Resp     — the list response wire type (defaults to `unknown`).
 */
export interface ResourceAdmin<Row, Values, Extras, Injected, Resp = unknown> {
  /** The always-shared List (every resource, including custom-view ones). */
  list: ListDescriptor<Row, Extras, Injected, Resp>;
  /** The default form-based create/edit. Omitted only when both views override. */
  form?: FormDescriptor<Row, Values>;
  /** Override: a custom create view replacing the default form create. */
  createView?: ComponentType<ResourceViewProps>;
  /** Override: a custom edit view replacing the default form edit. */
  editView?: ComponentType<ResourceViewProps>;
}
