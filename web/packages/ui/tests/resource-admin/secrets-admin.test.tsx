// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { SecretsAdmin } from '../../src/resource-admin/secrets-admin';

import type {
  Secret,
  SecretsAdminContextValue,
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

function makeValue(
  overrides: Partial<SecretsAdminContextValue['state']>,
): SecretsAdminContextValue {
  return {
    state: {
      secrets: [],
      isLoading: false,
      loadError: null,
      remove: { target: null, error: null, pending: false },
      filters: {},
      sort: null,
      pageSize: 25,
      scope: '',
      spaceOptions: [],
      pagination: { hasPrevPage: false, hasNextPage: false },
      ...overrides,
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

function renderTable(
  overrides: Partial<SecretsAdminContextValue['state']>,
  actions?: Partial<SecretsAdminContextValue['actions']>,
): SecretsAdminContextValue {
  const value = makeValue(overrides);
  if (actions) Object.assign(value.actions, actions);
  render(
    <SecretsAdmin.Provider value={value}>
      <SecretsAdmin.Root />
    </SecretsAdmin.Provider>,
  );
  return value;
}

describe('SecretsAdmin — set-only list (no value ever surfaced)', () => {
  it('lists secrets by metadata only — no value input in the read view', () => {
    renderTable({ secrets: [secret] });
    expect(screen.getByText('Stripe key')).toBeDefined();
    // No value is ever surfaced in the list: no inputs in the read view.
    expect(document.querySelector('input[type="password"]')).toBeNull();
  });
});

describe('SecretsAdmin — table', () => {
  it('renders generic empty copy while keeping the sortable headers mounted', () => {
    renderTable({ secrets: [] });
    expect(screen.getByText('No secrets yet.')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('keeps the header and filter row mounted while loading', () => {
    renderTable({ secrets: [], isLoading: true });
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    expect(screen.getByText('Loading secrets…')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
    expect(screen.getByRole('searchbox')).toBeDefined();
  });

  it('leaves the Space column blank for an org-direct secret', () => {
    renderTable({ secrets: [secret] });
    expect(screen.getByRole('columnheader', { name: 'Space' })).toBeDefined();
    expect(screen.queryByText('Organization')).toBeNull();
  });

  it('resolves a space-scoped secret name to its space label', () => {
    renderTable({ secrets: [spaceSecret], spaceOptions });
    expect(screen.getByText('Main')).toBeDefined();
  });

  it('hides the Space column inside a specific space (redundant there)', () => {
    renderTable({ secrets: [spaceSecret], spaceOptions, scope: 'main' });
    expect(screen.queryByRole('columnheader', { name: 'Space' })).toBeNull();
  });

  it('navigates to the routed edit page when the name link is clicked', () => {
    const openEdit = vi.fn();
    renderTable({ secrets: [secret] }, { openEdit });
    fireEvent.click(screen.getByRole('button', { name: 'Stripe key' }));
    expect(openEdit).toHaveBeenCalledWith(secret);
  });

  it('renders edit/delete as icon actions, with destructive styling on delete', () => {
    const openEdit = vi.fn();
    const openRemove = vi.fn();
    renderTable({ secrets: [secret] }, { openEdit, openRemove });

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
    renderTable({ secrets: [secret] }, { openCreate });
    fireEvent.click(screen.getByRole('button', { name: 'New secret' }));
    expect(openCreate).toHaveBeenCalledTimes(1);
  });
});

describe('SecretsAdmin — list controls', () => {
  it('toggles the filter row and reflects the on/off state', () => {
    renderTable({ secrets: [secret] });
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
      renderTable({ secrets: [secret] }, { setFilter });
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
    renderTable({ secrets: [secret] }, { toggleSort });

    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(toggleSort).toHaveBeenCalledWith('displayName');

    fireEvent.click(screen.getByRole('button', { name: 'Created' }));
    expect(toggleSort).toHaveBeenCalledWith('createTime');
  });

  it('distinguishes a filtered-empty result while keeping controls mounted', () => {
    renderTable({ secrets: [], filters: { displayName: 'zzz' } });
    expect(screen.getByText('No secrets match your filters.')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('shows Clear filters only when a filter or scope is active', () => {
    renderTable({ secrets: [secret] });
    expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull();
    cleanup();

    const clearFilters = vi.fn();
    renderTable(
      { secrets: [secret], filters: { displayName: 'stripe' } },
      { clearFilters },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(clearFilters).toHaveBeenCalled();
  });

  it('pages forward and back through the cursor pager', () => {
    const nextPage = vi.fn();
    const prevPage = vi.fn();
    renderTable(
      {
        secrets: [secret],
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

describe('SecretsAdmin — scope', () => {
  it('shows the scope combobox (resting on "All spaces") as a gated toolbar control', () => {
    renderTable({ secrets: [secret], spaceOptions });
    expect(screen.queryByPlaceholderText('All spaces')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    expect(screen.getByPlaceholderText('All spaces')).toBeDefined();
  });
});

describe('SecretsAdmin — quick delete', () => {
  it('opens the delete confirm with the referenced-by-connector warning', () => {
    renderTable({
      secrets: [secret],
      remove: { target: secret, error: null, pending: false },
    });
    expect(screen.getByText('Delete secret?')).toBeDefined();
    expect(
      screen.getByText(/still referenced by a connector can't be deleted/),
    ).toBeDefined();
  });
});
