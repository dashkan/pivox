'use client';

import { createContext, use } from 'react';

import type { GridContextValue } from './types';

/**
 * The grid's dependency-injection context. Held as `unknown`-rowed; `useGrid<T>`
 * narrows it at the call site. A provider is the ONLY place that knows how the
 * state is produced (state-decouple-implementation) — subcomponents read this
 * interface, never an implementation.
 */
const GridContext = createContext<GridContextValue<unknown> | null>(null);

export { GridContext };

/**
 * Read the injected grid interface. `use()` (React 19) rather than
 * `useContext()`. Throws outside a `<Grid.Provider>` so a misuse fails loudly.
 */
export function useGrid<T>(): GridContextValue<T> {
  const ctx = use(GridContext);
  if (!ctx) {
    throw new Error('Grid.* components must be used within a <Grid.Provider>.');
  }
  // eslint-disable-next-line typescript/no-unsafe-type-assertion -- one module-level context holds the value unknown-rowed (React context is invariant); useGrid<T> narrows back to the T the consumer's provider supplied. Standard generic-context DI boundary.
  return ctx as GridContextValue<T>;
}
