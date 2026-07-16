// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import {
  aipStringLiteral,
  cycleSort,
  orderByParam,
  useListControls,
} from '../../src/resource-admin/use-list-controls';

import type { ListControlsValue } from '../../src/resource-admin/types';

const base: ListControlsValue = {
  filters: {},
  sort: null,
  pageSize: 25,
  scope: '',
  pageToken: undefined,
};

const PUSH = { history: 'push' };
const REPLACE = { history: 'replace' };

function harness(value: ListControlsValue = base) {
  const onChange = vi.fn();
  const { result, rerender } = renderHook(
    (props: { value: ListControlsValue }) =>
      useListControls({ value: props.value, onChange }),
    { initialProps: { value } },
  );
  return { result, onChange, rerender };
}

describe('useListControls — controlled setters commit through onChange', () => {
  it('setFilter defaults to push, honors an explicit replace, and clears the cursor', () => {
    const push = harness({ ...base, pageToken: 'tok' });
    act(() => push.result.current.setFilter('displayName', 'stripe'));
    expect(push.onChange).toHaveBeenCalledWith(
      { ...base, filters: { displayName: 'stripe' }, pageToken: undefined },
      PUSH,
    );

    const replace = harness(base);
    act(() =>
      replace.result.current.setFilter('displayName', 'stripe', 'replace'),
    );
    expect(replace.onChange).toHaveBeenCalledWith(expect.anything(), REPLACE);
  });

  it('clearFilters pushes, resets filters + scope + cursor, keeps sort', () => {
    const { result, onChange } = harness({
      filters: { displayName: 'x', agent: 'a' },
      sort: { field: 'displayName', direction: 'asc' },
      pageSize: 50,
      scope: 'main',
      pageToken: 'tok',
    });
    act(() => result.current.clearFilters());
    expect(onChange).toHaveBeenCalledWith(
      {
        filters: {},
        sort: { field: 'displayName', direction: 'asc' },
        pageSize: 50,
        scope: '',
        pageToken: undefined,
      },
      PUSH,
    );
  });

  it('toggleSort / setPageSize / setScope push and clear the cursor', () => {
    const sort = harness({ ...base, pageToken: 'tok' });
    act(() => sort.result.current.toggleSort('displayName'));
    expect(sort.onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        sort: { field: 'displayName', direction: 'asc' },
        pageToken: undefined,
      }),
      PUSH,
    );

    const size = harness({ ...base, pageToken: 'tok' });
    act(() => size.result.current.setPageSize(100));
    expect(size.onChange).toHaveBeenCalledWith(
      expect.objectContaining({ pageSize: 100, pageToken: undefined }),
      PUSH,
    );

    const scope = harness({ ...base, pageToken: 'tok' });
    act(() => scope.result.current.setScope('main'));
    expect(scope.onChange).toHaveBeenCalledWith(
      expect.objectContaining({ scope: 'main', pageToken: undefined }),
      PUSH,
    );
  });
});

describe('useListControls — multi-step pagination', () => {
  it('prevPage walks back through the cursor stack (page 3 → 2 → 1), all push', () => {
    const { result, onChange, rerender } = harness(base); // page 1

    act(() => result.current.nextPage('t2'));
    expect(onChange).toHaveBeenLastCalledWith(
      { ...base, pageToken: 't2' },
      PUSH,
    );
    rerender({ value: { ...base, pageToken: 't2' } });

    act(() => result.current.nextPage('t3'));
    expect(onChange).toHaveBeenLastCalledWith(
      { ...base, pageToken: 't3' },
      PUSH,
    );
    rerender({ value: { ...base, pageToken: 't3' } });

    // Prev → the ACTUAL previous page (t2), not page 1.
    act(() => result.current.prevPage());
    expect(onChange).toHaveBeenLastCalledWith(
      { ...base, pageToken: 't2' },
      PUSH,
    );
    rerender({ value: { ...base, pageToken: 't2' } });

    // Prev again → page 1 (cursor cleared).
    act(() => result.current.prevPage());
    expect(onChange).toHaveBeenLastCalledWith(
      { ...base, pageToken: undefined },
      PUSH,
    );
  });

  it('hasPrevPage tracks the stack and a deep-linked cursor', () => {
    expect(harness(base).result.current.hasPrevPage).toBe(false);
    // Deep link onto a cursor with an empty stack still shows Prev.
    expect(
      harness({ ...base, pageToken: 't2' }).result.current.hasPrevPage,
    ).toBe(true);

    const h = harness(base);
    act(() => h.result.current.nextPage('t2'));
    expect(h.result.current.hasPrevPage).toBe(true);
  });

  it('falls back to page 1 for a deep-linked cursor with an empty stack', () => {
    const { result, onChange } = harness({ ...base, pageToken: 't5' });
    act(() => result.current.prevPage());
    expect(onChange).toHaveBeenCalledWith(
      { ...base, pageToken: undefined },
      PUSH,
    );
  });

  it('reconciles the stack when the URL cursor changes outside the setters (browser Back)', () => {
    // Build 1 → 2 → 3 through the setters.
    const { result, onChange, rerender } = harness(base);
    act(() => result.current.nextPage('t2'));
    rerender({ value: { ...base, pageToken: 't2' } });
    act(() => result.current.nextPage('t3'));
    rerender({ value: { ...base, pageToken: 't3' } });

    // Browser Back to page 2: the URL restores t2 WITHOUT prevPage. The stack
    // must trim so Prev targets page 1 (not the page we're already on — the
    // pre-fix off-by-one navigated back to t2).
    rerender({ value: { ...base, pageToken: 't2' } });
    expect(result.current.hasPrevPage).toBe(true);
    onChange.mockClear();
    act(() => result.current.prevPage());
    expect(onChange).toHaveBeenLastCalledWith(
      { ...base, pageToken: undefined },
      PUSH,
    );
  });

  it('hides Prev after an external jump to page 1 with a stale stack', () => {
    // 1 → 2 → 3 through the setters leaves a two-entry back-stack.
    const { result, rerender } = harness(base);
    act(() => result.current.nextPage('t2'));
    rerender({ value: { ...base, pageToken: 't2' } });
    act(() => result.current.nextPage('t3'));
    rerender({ value: { ...base, pageToken: 't3' } });

    // Edit the URL straight back to page 1 (cursor cleared) — no setter. The
    // pre-fix bug left hasPrevPage true (stale stack) and showed Prev on page 1.
    rerender({ value: { ...base, pageToken: undefined } });
    expect(result.current.hasPrevPage).toBe(false);
  });

  it('a scope change clears the back-stack', () => {
    const h = harness(base);
    act(() => h.result.current.nextPage('t2'));
    h.rerender({ value: { ...base, pageToken: 't2' } });
    expect(h.result.current.hasPrevPage).toBe(true);

    act(() => h.result.current.setScope('main'));
    // The route applies the committed value (cursor cleared); stack is empty.
    h.rerender({ value: { ...base, scope: 'main', pageToken: undefined } });
    expect(h.result.current.hasPrevPage).toBe(false);
  });
});

describe('cycleSort', () => {
  it('cycles unsorted → asc → desc → unsorted per field', () => {
    expect(cycleSort(null, 'a')).toEqual({ field: 'a', direction: 'asc' });
    expect(cycleSort({ field: 'a', direction: 'asc' }, 'a')).toEqual({
      field: 'a',
      direction: 'desc',
    });
    expect(cycleSort({ field: 'a', direction: 'desc' }, 'a')).toBeNull();
    expect(cycleSort({ field: 'a', direction: 'desc' }, 'b')).toEqual({
      field: 'b',
      direction: 'asc',
    });
  });
});

describe('orderByParam', () => {
  it('maps sort state to an AIP-160 order_by expression', () => {
    expect(orderByParam(null)).toBeUndefined();
    expect(orderByParam({ field: 'displayName', direction: 'asc' })).toBe(
      'displayName',
    );
    expect(orderByParam({ field: 'updateTime', direction: 'desc' })).toBe(
      'updateTime desc',
    );
  });
});

describe('aipStringLiteral', () => {
  it('quotes and escapes so a typed quote cannot break out', () => {
    expect(aipStringLiteral('hub')).toBe('"hub"');
    expect(aipStringLiteral('a"b')).toBe('"a\\"b"');
    expect(aipStringLiteral('a\\b')).toBe('"a\\\\b"');
    expect(aipStringLiteral('')).toBe('""');
  });
});
