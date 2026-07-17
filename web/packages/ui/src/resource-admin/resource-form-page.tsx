'use client';

import { FormPage, useFormPage } from '../form-page';

import { AdminNotice } from './admin-frame';

import type { ReactNode } from 'react';

/**
 * AIP create/edit shell over the generic `FormPage` — the form-side twin of the
 * connectors admin's grid consumer. Two EXPLICIT variant components
 * (`patterns-explicit-variants`), NOT one component switched by a `mode` prop:
 * the create tree never composes `FormPage.Delete`, so delete-on-edit is an
 * affordance of composition, not a boolean (`architecture-avoid-boolean-props`).
 *
 * These variants render INSIDE the resource's own form provider (which supplies
 * `FormPage.Provider` + the resource-owned values context), reading the standard
 * copy from `meta.resourceLabel` via `useFormPage`. There is deliberately no
 * scoped tier above this one — scope is a route + consumer concern carried in
 * the resource form values, never a shell tier (see the design doc).
 *
 * ```tsx
 * <ConnectorFormProvider value={…}>
 *   <ResourceFormPage.Create back={<Link to={returnTo}>← Connectors</Link>}>
 *     <ConnectorCreateFields />
 *   </ResourceFormPage.Create>
 * </ConnectorFormProvider>
 * ```
 */

/**
 * Create variant: "New {label}" title, "Create {label}" submit, Cancel. Wires
 * nothing itself — `submit` / `cancel` come from the injected contract; the
 * provider calls the route's `onSubmitSuccess` in the mutation's `onSuccess`.
 */
function ResourceFormPageCreate({
  children,
  back,
}: {
  /** The resource's create field-set, slotted into `FormPage.Body`. */
  children: ReactNode;
  /** Route-composed back link (a router `<Link>`), kept out of the shell. */
  back?: ReactNode;
}) {
  const { meta } = useFormPage<unknown>();
  return (
    <FormPage.Frame>
      <FormPage.Header back={back}>New {meta.resourceLabel}</FormPage.Header>
      <FormPage.Body>{children}</FormPage.Body>
      <FormPage.Actions>
        <FormPage.Cancel>Cancel</FormPage.Cancel>
        <FormPage.Submit>Create {meta.resourceLabel}</FormPage.Submit>
      </FormPage.Actions>
    </FormPage.Frame>
  );
}

/**
 * Edit variant: the record-load states (spinner / error notice), then the same
 * parts + `FormPage.Delete` ("Delete {label}") and a "Save changes" submit. The
 * delete-confirm DIALOG itself is composed by the resource as a sibling inside
 * the provider (its copy + failure text are resource-specific — a connector
 * delete warns that referencing activities will fail), which `FormPage.Delete`
 * opens via the injected `actions.delete`.
 */
function ResourceFormPageEdit({
  children,
  back,
}: {
  /** The resource's edit field-set, slotted into `FormPage.Body`. */
  children: ReactNode;
  back?: ReactNode;
}) {
  const { state, meta } = useFormPage<unknown>();

  // Load-state gate (rendering-conditional-render): while the edit record loads
  // or fails, show a notice instead of a form seeded from missing data.
  if (state.recordLoading) {
    return (
      <div className="flex flex-1 flex-col gap-6 p-6">
        <AdminNotice>Loading {meta.resourceLabel}…</AdminNotice>
      </div>
    );
  }
  if (state.loadError !== null) {
    return (
      <div className="flex flex-1 flex-col gap-6 p-6">
        <AdminNotice>{state.loadError}</AdminNotice>
      </div>
    );
  }

  return (
    <FormPage.Frame>
      <FormPage.Header back={back}>Edit {meta.resourceLabel}</FormPage.Header>
      <FormPage.Body>{children}</FormPage.Body>
      <FormPage.Actions>
        <FormPage.Delete>Delete {meta.resourceLabel}</FormPage.Delete>
        <FormPage.Cancel>Cancel</FormPage.Cancel>
        <FormPage.Submit>Save changes</FormPage.Submit>
      </FormPage.Actions>
    </FormPage.Frame>
  );
}

/** The compound AIP form-page shell. */
export const ResourceFormPage = {
  Create: ResourceFormPageCreate,
  Edit: ResourceFormPageEdit,
};
