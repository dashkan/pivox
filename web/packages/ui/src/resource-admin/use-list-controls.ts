'use client';

import { useCallback, useMemo, useRef, useState } from 'react';

import type {
  HistoryMode,
  ListControlsChange,
  ListControlsValue,
  SortDirection,
  SortState,
} from './types';

/** Selectable page sizes for the list pager. */
export const PAGE_SIZES = [10, 25, 50, 100] as const;
export const DEFAULT_PAGE_SIZE = 25;

/** `field` → AIP-160 `order_by`; descending gets the ` desc` suffix. */
export function orderByParam(sort: SortState | null): string | undefined {
  if (!sort) return undefined;
  return sort.direction === 'desc' ? `${sort.field} desc` : sort.field;
}

/**
 * Quotes `value` as an AIP-160 string literal, escaping backslashes and double
 * quotes so a user-typed `"` can't break out of the filter expression.
 */
export function aipStringLiteral(value: string): string {
  return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
}

/** Cycle a column's sort: unsorted → asc → desc → unsorted. */
export function cycleSort(
  current: SortState | null,
  field: string,
): SortState | null {
  if (!current || current.field !== field) {
    return { field, direction: 'asc' satisfies SortDirection };
  }
  if (current.direction === 'asc') return { field, direction: 'desc' };
  return null;
}

export interface ListControls {
  /** Committed per-field filter values (the caller's source of truth). */
  filters: Record<string, string>;
  /** Set (or clear, with '') one field's filter value. Resets the cursor. */
  setFilter: (field: string, value: string, history?: HistoryMode) => void;
  /** Reset all filters and scope to defaults (sort is left alone). Resets the cursor. */
  clearFilters: () => void;
  /** Active column sort, or null for the server's default order. */
  sort: SortState | null;
  /** Cycle a column: unsorted → asc → desc → unsorted. Resets the cursor. */
  toggleSort: (field: string) => void;
  /** Rows requested per page. */
  pageSize: number;
  setPageSize: (size: number) => void;
  /** List parent selector: empty is the org rollup; non-empty is a space slug. Resets the cursor. */
  scope: string;
  setScope: (scope: string) => void;
  /** Opaque cursor for the current page; undefined is the first page. */
  pageToken: string | undefined;
  /** Advance to the page named by a response's next-page token. */
  nextPage: (token: string) => void;
  /** Step back to the previous page (multi-step via the cursor back-stack). */
  prevPage: () => void;
  /** Whether a previous page exists (a back-stack entry or a deep-linked cursor). */
  hasPrevPage: boolean;
}

/**
 * Controlled list-controls adapter. The source-of-truth state lives with the
 * caller (`value`, e.g. URL search params); every mutation goes through
 * `onChange`, which also tells the caller how to record it in history (discrete
 * changes `push`, debounced text `replace`) — so the package stays
 * router-agnostic.
 *
 * Pagination keeps an ephemeral cursor back-stack in local state so Prev walks
 * to the ACTUAL previous page (page 3 → 2 → 1). The current page's cursor stays
 * in `value` for SSR/deep-linking; a filter/sort/scope/size change clears both
 * the cursor and the stack. On a fresh load of a deep cursor (empty stack), Prev
 * falls back to the first page.
 *
 * The stack is local state but the current cursor is `value.pageToken`, owned by
 * the caller (URL). When that cursor changes OUTSIDE our setters — browser
 * Back/Forward, a deep-linked or hand-edited URL — the stack is reconciled
 * during render (React's adjust-state-on-prop-change pattern): a signature
 * (`syncedToken`) records the cursor the stack was last reconciled against, and
 * a `pending` ref marks cursors our own setters are navigating to (so an
 * internal navigation, whose stack is already correct, isn't mistaken for an
 * external one). On an external change we trim the stack back to the page we
 * landed on if it's still in history, else drop it (Prev falls back to page 1).
 */
export function useListControls({
  value,
  onChange,
}: {
  value: ListControlsValue;
  onChange: ListControlsChange;
}): ListControls {
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  // The first page's cursor is `undefined` in `value` but `''` in the stack;
  // normalize so the two representations compare cleanly.
  const [syncedToken, setSyncedToken] = useState(value.pageToken ?? '');
  // Cursor our own setter is navigating to; consumed by the reconcile below so
  // an internal navigation doesn't look like an external one. A ref (not state)
  // so setting it never triggers a render and it survives the interim render
  // where our local stack update lands before `value` catches up.
  const pending = useRef<string | null>(null);

  // Reconcile the back-stack with the URL cursor when it changed outside our
  // setters. Runs during render; the `token !== syncedToken` guard makes it
  // idempotent (no update loop).
  const token = value.pageToken ?? '';
  if (token !== syncedToken) {
    if (pending.current !== token) {
      // External change (Back/Forward, deep link). Trim to the landed page if
      // it's still in the stack; otherwise drop the in-session history.
      const idx = cursorStack.indexOf(token);
      setCursorStack(idx >= 0 ? cursorStack.slice(0, idx) : []);
    }
    setSyncedToken(token);
    pending.current = null;
  }

  // A filter/sort/scope/size change invalidates pagination: drop the stack and
  // (via the setters, which zero pageToken) the cursor.
  const commit = useCallback(
    (next: ListControlsValue, history: HistoryMode) => {
      pending.current = ''; // navigating to the first page
      setCursorStack([]);
      onChange(next, { history });
    },
    [onChange],
  );

  const setFilter = useCallback(
    (field: string, next: string, history: HistoryMode = 'push') => {
      commit(
        {
          ...value,
          filters: { ...value.filters, [field]: next },
          pageToken: undefined,
        },
        history,
      );
    },
    [value, commit],
  );

  const clearFilters = useCallback(() => {
    commit(
      { ...value, filters: {}, scope: '', pageToken: undefined },
      'push',
    );
  }, [value, commit]);

  const toggleSort = useCallback(
    (field: string) => {
      commit(
        { ...value, sort: cycleSort(value.sort, field), pageToken: undefined },
        'push',
      );
    },
    [value, commit],
  );

  const setPageSize = useCallback(
    (size: number) => {
      commit({ ...value, pageSize: size, pageToken: undefined }, 'push');
    },
    [value, commit],
  );

  const setScope = useCallback(
    (scope: string) => {
      commit({ ...value, scope, pageToken: undefined }, 'push');
    },
    [value, commit],
  );

  const nextPage = useCallback(
    (next: string) => {
      pending.current = next;
      // Remember the page we're leaving ('' represents the first page).
      setCursorStack((stack) => [...stack, value.pageToken ?? '']);
      onChange({ ...value, pageToken: next }, { history: 'push' });
    },
    [value, onChange],
  );

  const prevPage = useCallback(() => {
    // Walk back to the previous page's cursor; empty stack → first page.
    const target = cursorStack.at(-1) || undefined;
    pending.current = target ?? '';
    setCursorStack((stack) => stack.slice(0, -1));
    onChange({ ...value, pageToken: target }, { history: 'push' });
  }, [value, onChange, cursorStack]);

  return useMemo(
    () => ({
      filters: value.filters,
      setFilter,
      clearFilters,
      sort: value.sort,
      toggleSort,
      pageSize: value.pageSize,
      setPageSize,
      scope: value.scope,
      setScope,
      pageToken: value.pageToken,
      nextPage,
      prevPage,
      hasPrevPage: cursorStack.length > 0 || Boolean(value.pageToken),
    }),
    [
      value,
      setFilter,
      clearFilters,
      toggleSort,
      setPageSize,
      setScope,
      nextPage,
      prevPage,
      cursorStack,
    ],
  );
}
