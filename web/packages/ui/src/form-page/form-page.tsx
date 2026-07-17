'use client';

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@pivox/primitives/alert-dialog';
import { Button } from '@pivox/primitives/button';
import { FieldError } from '@pivox/primitives/field';
import { useEffect, useState } from 'react';

import { FormPageContext, useFormPage } from './form-page.context';

import type { FormPageContextValue } from './types';
import type { ReactNode } from 'react';

/**
 * Generic, dumb, controlled form page — a compound component
 * (architecture-compound-components). It fetches nothing, owns no form values,
 * runs no mutation, and imports no router. It renders a page frame + action bar
 * and calls injected handlers. A `FormPage.Provider` injects the
 * `{ state, actions, meta }` interface (state-context-interface); every part
 * reads it via `useFormPage()`. Consumers compose exactly the parts they want —
 * no boolean toggles (architecture-avoid-boolean-props).
 *
 * Create and edit are EXPLICIT variants (patterns-explicit-variants): the create
 * tree simply doesn't compose `FormPage.Delete`, so the delete affordance is
 * absent, not disabled. This is the composition skill's ChannelComposer /
 * EditComposer pattern applied verbatim.
 *
 * ```tsx
 * <FormPage.Provider value={value}>   // maps resource state → FormPageContextValue<T>
 *   <FormPage.Frame>
 *     <FormPage.Header>New connector</FormPage.Header>
 *     <FormPage.Body><ConnectorCreateFields /></FormPage.Body>
 *     <FormPage.Actions>
 *       <FormPage.Cancel>Cancel</FormPage.Cancel>
 *       <FormPage.Submit>Create connector</FormPage.Submit>
 *     </FormPage.Actions>
 *   </FormPage.Frame>
 * </FormPage.Provider>
 * ```
 */

/**
 * DI boundary: the only place that knows how state is produced. Also the home of
 * the router-free navigate-away guards that must live at the FormPage level:
 *
 *  - `onDirtyChange` reports the derived `dirty` up so a router-specific blocker
 *    (start `useBlocker`, electron history guard) can live in the route. This is
 *    the ONE sanctioned effect — syncing an external router system to
 *    React-derived state, the inverse-subscription case react.dev allows. It
 *    does no React state work.
 *  - `beforeunload` is subscribed ONLY while dirty (hard reload / close / quit),
 *    a genuine external-system (window) subscription; it registers/cleans up on
 *    the `dirty` transition.
 */
function FormPageProvider<T>({
  value,
  children,
}: {
  value: FormPageContextValue<T>;
  children: ReactNode;
}) {
  const { dirty } = value.state;
  const { onDirtyChange } = value.meta;

  // Soft in-app navigation blocker lives in the route; it only needs the dirty
  // signal. Reporting derived state up is the inverse-subscription effect
  // react.dev sanctions (see design "Dirty-state guard"). Flagged for review as
  // the one defensible effect.
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  // Hard unload (reload / close tab / quit) — a real external-system (window)
  // subscription, registered only while dirty. No React state work here.
  useEffect(() => {
    if (!dirty) return undefined;
    const handler = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      // Legacy browsers require a truthy returnValue to show the prompt.
      event.returnValue = '';
    };
    globalThis.addEventListener('beforeunload', handler);
    return () => globalThis.removeEventListener('beforeunload', handler);
  }, [dirty]);

  // eslint-disable-next-line typescript/no-unsafe-type-assertion -- widen the consumer's typed value to the unknown-rowed context (React context is invariant); useFormPage<T> re-narrows. The DI boundary needs this cast.
  const injected = value as FormPageContextValue<unknown>;
  return <FormPageContext value={injected}>{children}</FormPageContext>;
}

/**
 * Owns the native `<form>` so Enter-to-submit and `type="submit"` work without a
 * manual keydown handler; `onSubmit` calls the injected `actions.submit()`.
 * Mirrors `Composer.Frame`. Header/Body/Actions render inside it.
 */
function FormPageFrame({ children }: { children: ReactNode }) {
  const { state, actions } = useFormPage<unknown>();
  return (
    <form
      className="flex flex-1 flex-col gap-6 p-6"
      onSubmit={(event) => {
        event.preventDefault();
        // Guard here too, not just via the disabled Submit button: this is a
        // reusable primitive, and Enter-to-submit / a second composed submit
        // control must not fire an invalid or in-flight write.
        if (!state.canSubmit || state.pending) return;
        actions.submit();
      }}
    >
      <div className="mx-auto flex w-full max-w-2xl flex-col gap-6">
        {children}
      </div>
    </form>
  );
}

/**
 * Title + optional Back affordance. `back` is a composed node the route supplies
 * (a router `<Link>`), never a router import here — keeping FormPage router-free.
 */
function FormPageHeader({
  children,
  back,
}: {
  children: ReactNode;
  /** A back link the route composes (e.g. `<Link to={returnTo}>← Connectors</Link>`). */
  back?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      {back !== undefined ? (
        <div className="text-sm text-muted-foreground">{back}</div>
      ) : null}
      <h1 className="text-2xl font-semibold tracking-tight">{children}</h1>
    </div>
  );
}

/**
 * Slot for the resource form fields — plain `children`, no render prop. The
 * fields are static composition that read the resource-owned form context
 * directly; there is deliberately no `renderForm(values)` prop
 * (patterns-children-over-render-props — and, unlike Grid's per-row `cell`,
 * there is no per-item data here so no carve-out is needed).
 */
function FormPageBody({ children }: { children: ReactNode }) {
  return <div className="flex flex-col gap-4">{children}</div>;
}

/** The action bar: renders `state.error` inline, then composes the buttons. */
function FormPageActions({ children }: { children: ReactNode }) {
  const { state } = useFormPage<unknown>();
  return (
    <div className="flex flex-col gap-3">
      {state.error !== null ? <FieldError>{state.error}</FieldError> : null}
      <div className="flex items-center justify-end gap-2">{children}</div>
    </div>
  );
}

/**
 * Cancel: reads `actions.cancel`. When `state.dirty`, routes through an
 * unsaved-changes confirm (a confirm, not a form — same shape as `DeleteDialog`)
 * before abandoning. Router-free and cross-env. `pending` gates it so a cancel
 * can't race an in-flight write. Label is its children.
 */
function FormPageCancel({ children }: { children: ReactNode }) {
  const { state, actions, meta } = useFormPage<unknown>();
  const [confirmOpen, setConfirmOpen] = useState(false);

  const onClick = () => {
    // Logic in the handler (5.8), not an effect watching a flag.
    if (state.dirty) {
      setConfirmOpen(true);
      return;
    }
    actions.cancel();
  };

  return (
    <>
      <Button
        type="button"
        variant="outline"
        onClick={onClick}
        disabled={state.pending}
      >
        {children}
      </Button>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Discard unsaved changes?</AlertDialogTitle>
            <AlertDialogDescription>
              You have unsaved edits to this {meta.resourceLabel}. Leaving now
              discards them.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep editing</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                setConfirmOpen(false);
                actions.cancel();
              }}
            >
              Discard changes
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/**
 * Submit: a plain `<button type="submit">` (the native form drives submission).
 * Reads `state.canSubmit` + `state.pending` from the generic context; the
 * resource-owned provider that built `submit` already holds the values, so this
 * button never sees them. Label is its children; `pending` swaps in "Saving…".
 * Absorbs today's `FormActions` submit button.
 */
function FormPageSubmit({ children }: { children: ReactNode }) {
  const { state } = useFormPage<unknown>();
  return (
    <Button type="submit" disabled={state.pending || !state.canSubmit}>
      {state.pending ? 'Saving…' : children}
    </Button>
  );
}

/**
 * Delete: reads `actions.delete`. Composed ONLY by the edit variant — its
 * presence IS the "delete on edit" affordance; there is no `showDelete` flag
 * (architecture-avoid-boolean-props). The injected `actions.delete` opens the
 * resource-owned delete-confirm (whose copy + failure text are resource-specific
 * — a connector delete warns that "activities that reference it will fail"), so
 * this stays a lean trigger and the generic contract holds no delete state.
 * Renders nothing if no delete was injected (defensive; edit always injects it).
 * Label is its children.
 */
function FormPageDelete({ children }: { children: ReactNode }) {
  const { state, actions } = useFormPage<unknown>();
  if (actions.delete === undefined) return null;
  const onDelete = actions.delete;
  return (
    <Button
      type="button"
      variant="destructive"
      className="mr-auto"
      onClick={onDelete}
      disabled={state.pending}
    >
      {children}
    </Button>
  );
}

/** The compound form page. Consumers compose the parts they want. */
export const FormPage = {
  Provider: FormPageProvider,
  Frame: FormPageFrame,
  Header: FormPageHeader,
  Body: FormPageBody,
  Actions: FormPageActions,
  Cancel: FormPageCancel,
  Submit: FormPageSubmit,
  Delete: FormPageDelete,
};
