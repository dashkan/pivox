'use client';

import { createContext, use } from 'react';

import type { FormPageContextValue } from './types';

/**
 * The form page's dependency-injection context. Held as `unknown`-rowed;
 * `useFormPage<T>` narrows it at the call site. A provider is the ONLY place
 * that knows how the state is produced (state-decouple-implementation) —
 * subcomponents read this interface, never an implementation. Identical DI
 * boundary to `GridContext`.
 */
const FormPageContext = createContext<FormPageContextValue<unknown> | null>(null);

export { FormPageContext };

/**
 * Read the injected form-page interface. `use()` (React 19) rather than
 * `useContext()`. Throws outside a `<FormPage.Provider>` so a misuse fails loudly.
 */
export function useFormPage<T>(): FormPageContextValue<T> {
  const ctx = use(FormPageContext);
  if (!ctx) {
    throw new Error(
      'FormPage.* components must be used within a <FormPage.Provider>.',
    );
  }
  // eslint-disable-next-line typescript/no-unsafe-type-assertion -- one module-level context holds the value unknown-rowed (React context is invariant); useFormPage<T> narrows back to the T the consumer's provider supplied. Standard generic-context DI boundary.
  return ctx as FormPageContextValue<T>;
}
