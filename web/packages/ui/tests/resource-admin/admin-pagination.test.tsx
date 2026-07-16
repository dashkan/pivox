// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

import { AdminPagination } from '../../src/resource-admin/admin-pagination';

// Radix Select measures the DOM; jsdom needs a ResizeObserver shim.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

function renderPager(
  props: Partial<React.ComponentProps<typeof AdminPagination>> = {},
) {
  const merged = {
    pageSize: 25,
    onPageSizeChange: vi.fn(),
    hasPrevPage: false,
    hasNextPage: false,
    onPrev: vi.fn(),
    onNext: vi.fn(),
    ...props,
  };
  // A form makes Radix Select render its hidden native mirror, so the value
  // can be changed without driving the pointer-based popover in jsdom.
  render(
    <form>
      <AdminPagination {...merged} />
    </form>,
  );
  return merged;
}

describe('AdminPagination', () => {
  it('shows the current page size', () => {
    renderPager({ pageSize: 25 });
    expect(
      screen.getByRole('combobox', { name: 'Rows per page' }).textContent,
    ).toContain('25');
  });

  it('reports a new page size through onPageSizeChange', () => {
    const onPageSizeChange = vi.fn();
    renderPager({ onPageSizeChange });
    // Radix mirrors options into a hidden native select for a11y/forms.
    const native = document.querySelector('select');
    expect(native).not.toBeNull();
    fireEvent.change(native as HTMLSelectElement, { target: { value: '100' } });
    expect(onPageSizeChange).toHaveBeenCalledWith(100);
  });

  it('disables Previous/Next according to page availability', () => {
    renderPager({ hasPrevPage: false, hasNextPage: true });
    expect(
      screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
    ).toBe(true);
    expect(
      screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled'),
    ).toBe(false);
  });
});
