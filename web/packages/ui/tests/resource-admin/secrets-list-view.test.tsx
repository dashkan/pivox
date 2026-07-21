// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { ResourceList } from '../../src/resource-admin/resource-list';
import {
  secretDeleteDescription,
  secretsListView,
} from '../../src/resource-admin/secrets-list-view';

import type { ResourceListContextValue } from '../../src/resource-admin/resource-list.context';
import type {
  RemoveState,
  Secret,
  SecretListExtras,
  SortState,
} from '../../src/resource-admin/types';

// Radix Select + Base UI combobox measure/scroll the DOM; jsdom needs shims.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

afterEach(cleanup);

const noop = (): void => {};

type Value = ResourceListContextValue<Secret, SecretListExtras>;

interface StateOverrides {
  rows?: Secret[];
  isLoading?: boolean;
  loadError?: string | null;
  remove?: RemoveState<Secret>;
  extras?: Partial<SecretListExtras>;
  filters?: Record<string, string>;
  sort?: SortState | null;
  pageSize?: number;
  scope?: string;
  pagination?: { hasPrevPage: boolean; hasNextPage: boolean };
}

/**
 * The secrets LIST — now the generic `ResourceList` driven by the
 * `secretsListView` descriptor + a DI'd resource-list value. This is the DOM
 * proof of the byte-identical migration off the old hand-written `SecretsAdmin`
 * bridge: the same metadata-only rows (the write-only value never surfaces), the
 * org rollup Space column, scope, sort, pagination, and quick row-delete
 * behaviors, verified against the generic composite.
 */
function makeValue(overrides: StateOverrides): Value {
  const { extras: extrasOverride, ...stateOverride } = overrides;
  return {
    state: {
      rows: [],
      isLoading: false,
      loadError: null,
      remove: { target: null, error: null, pending: false },
      extras: { spaceOptions: [], ...extrasOverride },
      filters: {},
      sort: null,
      pageSize: 25,
      scope: '',
      pagination: { hasPrevPage: false, hasNextPage: false },
      ...stateOverride,
    },
    actions: {
      openCreate: noop,
      openEdit: noop,
      openRemove: noop,
      closeRemove: noop,
      confirmRemove: noop,
      setFilter: noop,
      clearFilters: noop,
      toggleSort: noop,
      setPageSize: noop,
      setScope: noop,
      nextPage: noop,
      prevPage: noop,
    },
  };
}

const secret: Secret = {
  name: 'organizations/acme/secrets/stripe-key',
  displayName: 'Stripe key',
  createTime: '2026-01-01T00:00:00Z',
  updateTime: '2026-02-01T00:00:00Z',
  etag: 'e1',
};

const spaceSecret: Secret = {
  name: 'organizations/acme/spaces/main/secrets/vizrt-key',
  displayName: 'VizRT key',
  createTime: '2026-01-01T00:00:00Z',
  updateTime: '2026-02-01T00:00:00Z',
};

const spaceOptions = [
  { name: 'organizations/acme/spaces/main', slug: 'main', displayName: 'Main' },
];

// Secrets compose the 90% preset: New button + edit+delete affordance column +
// confirm dialog. This is the composed-affordance twin of the old newLabel/
// deleteConfirm descriptor flags.
function renderList(value: Value) {
  render(
    <ResourceList.Provider value={value}>
      <ResourceList.Default
        view={secretsListView}
        noun="secret"
        confirmDelete={secretDeleteDescription}
      />
    </ResourceList.Provider>,
  );
}

function renderTable(
  overrides: StateOverrides,
  actions?: Partial<Value['actions']>,
): Value {
  const value = makeValue(overrides);
  if (actions) Object.assign(value.actions, actions);
  renderList(value);
  return value;
}

describe('Secrets list view — set-only list (no value ever surfaced)', () => {
  it('lists secrets by metadata only — no value input in the read view', () => {
    renderTable({ rows: [secret] });
    expect(screen.getByText('Stripe key')).toBeDefined();
    // No value is ever surfaced in the list: no inputs in the read view.
    expect(document.querySelector('input[type="password"]')).toBeNull();
  });

  it('advertises the write-only value in the description', () => {
    renderTable({ rows: [secret] });
    expect(screen.getByText(/Values are write-only/)).toBeDefined();
  });
});

describe('Secrets list view — table', () => {
  it('renders generic empty copy while keeping the sortable headers mounted', () => {
    renderList(makeValue({ rows: [] }));
    expect(screen.getByText('No secrets yet.')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('keeps the header and filter row mounted while loading', () => {
    renderList(makeValue({ rows: [], isLoading: true }));
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    expect(screen.getByText('Loading secrets…')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
    expect(screen.getByRole('searchbox')).toBeDefined();
  });

  it('leaves the Space column blank for an org-direct secret', () => {
    renderTable({ rows: [secret] });
    expect(screen.getByRole('columnheader', { name: 'Space' })).toBeDefined();
    expect(screen.queryByText('Organization')).toBeNull();
  });

  it('resolves a space-scoped secret name to its space label', () => {
    renderTable({ rows: [spaceSecret], extras: { spaceOptions } });
    expect(screen.getByText('Main')).toBeDefined();
  });

  it('falls back to the space slug when the space is unresolved', () => {
    renderTable({ rows: [spaceSecret] });
    expect(screen.getByText('main')).toBeDefined();
  });

  it('hides the Space column inside a specific space (redundant there)', () => {
    renderTable({ rows: [spaceSecret], extras: { spaceOptions }, scope: 'main' });
    expect(screen.queryByRole('columnheader', { name: 'Space' })).toBeNull();
  });

  it('navigates to the routed edit page when the name link is clicked', () => {
    const openEdit = vi.fn();
    renderTable({ rows: [secret] }, { openEdit });
    fireEvent.click(screen.getByRole('button', { name: 'Stripe key' }));
    expect(openEdit).toHaveBeenCalledWith(secret);
  });

  it('renders edit/delete as icon actions, with destructive styling on delete', () => {
    const openEdit = vi.fn();
    const openRemove = vi.fn();
    renderTable({ rows: [secret] }, { openEdit, openRemove });

    const edit = screen.getByRole('button', { name: 'Edit secret' });
    const remove = screen.getByRole('button', { name: 'Delete secret' });
    expect(edit.textContent).toBe('');
    expect(remove.textContent).toBe('');
    expect(remove.className).toContain('text-destructive');

    fireEvent.click(edit);
    expect(openEdit).toHaveBeenCalledWith(secret);
    fireEvent.click(remove);
    expect(openRemove).toHaveBeenCalledWith(secret);
  });

  it('navigates to the routed create page from the "New secret" button', () => {
    const openCreate = vi.fn();
    renderTable({ rows: [secret] }, { openCreate });
    fireEvent.click(screen.getByRole('button', { name: 'New secret' }));
    expect(openCreate).toHaveBeenCalledTimes(1);
  });
});

describe('Secrets list view — list controls', () => {
  it('toggles the filter row and reflects the on/off state', () => {
    renderTable({ rows: [secret] });
    const button = screen.getByRole('button', { name: 'Filter' });
    expect(screen.queryByRole('searchbox')).toBeNull();
    expect(button.getAttribute('aria-pressed')).toBe('false');

    fireEvent.click(button);
    expect(screen.getByRole('searchbox')).toBeDefined();
    expect(button.getAttribute('aria-pressed')).toBe('true');
  });

  it('drives the name filter through setFilter (debounced)', () => {
    vi.useFakeTimers();
    try {
      const setFilter = vi.fn();
      renderTable({ rows: [secret] }, { setFilter });
      fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
      fireEvent.change(screen.getByRole('searchbox'), {
        target: { value: 'stripe' },
      });
      expect(setFilter).not.toHaveBeenCalled();
      act(() => vi.advanceTimersByTime(300));
      expect(setFilter).toHaveBeenCalledWith('displayName', 'stripe', 'replace');
    } finally {
      vi.useRealTimers();
    }
  });

  it('renders sortable Name and Created headers wired to toggleSort', () => {
    const toggleSort = vi.fn();
    renderTable({ rows: [secret] }, { toggleSort });

    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(toggleSort).toHaveBeenCalledWith('displayName');

    fireEvent.click(screen.getByRole('button', { name: 'Created' }));
    expect(toggleSort).toHaveBeenCalledWith('createTime');
  });

  it('marks the active sort column via aria-sort', () => {
    renderTable({
      rows: [secret],
      sort: { field: 'createTime', direction: 'desc' },
    });
    expect(
      screen.getByRole('columnheader', { name: 'Created' }).getAttribute('aria-sort'),
    ).toBe('descending');
    expect(
      screen.getByRole('columnheader', { name: 'Name' }).getAttribute('aria-sort'),
    ).toBe('none');
  });

  it('distinguishes a filtered-empty result while keeping controls mounted', () => {
    renderTable({ rows: [], filters: { displayName: 'zzz' } });
    expect(screen.getByText('No secrets match your filters.')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('shows Clear filters only when a filter or scope is active', () => {
    renderTable({ rows: [secret] });
    expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull();
    cleanup();

    const clearFilters = vi.fn();
    renderTable(
      { rows: [secret], filters: { displayName: 'stripe' } },
      { clearFilters },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(clearFilters).toHaveBeenCalled();
  });

  it('shows Clear filters when a non-default scope is active', () => {
    renderTable({ rows: [secret], scope: 'main' });
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeDefined();
  });

  it('pages forward and back through the cursor pager', () => {
    const nextPage = vi.fn();
    const prevPage = vi.fn();
    renderTable(
      {
        rows: [secret],
        pagination: { hasPrevPage: true, hasNextPage: true },
      },
      { nextPage, prevPage },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(nextPage).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(prevPage).toHaveBeenCalled();
  });
});

describe('Secrets list view — scope', () => {
  it('shows the scope combobox (resting on "All spaces") as a gated toolbar control', () => {
    renderTable({ rows: [secret], extras: { spaceOptions } });
    expect(screen.queryByPlaceholderText('All spaces')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    expect(screen.getByPlaceholderText('All spaces')).toBeDefined();
  });
});

describe('Secrets list view — quick delete', () => {
  it('opens the delete confirm with the referenced-by-connector warning', () => {
    renderTable({
      rows: [secret],
      remove: { target: secret, error: null, pending: false },
    });
    expect(screen.getByText('Delete secret?')).toBeDefined();
    expect(
      screen.getByText(/still referenced by a connector can't be deleted/),
    ).toBeDefined();
  });
});
