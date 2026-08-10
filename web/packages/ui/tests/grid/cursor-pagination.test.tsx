// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { CursorPager, PageSizeSelect } from '../../src/grid/cursor-pagination';

// Radix Select measures the DOM; jsdom needs a ResizeObserver shim.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

describe('PageSizeSelect', () => {
  function renderSelect(
    props: Partial<React.ComponentProps<typeof PageSizeSelect>> = {},
  ) {
    const merged = { pageSize: 25, onPageSizeChange: vi.fn(), ...props };
    // A form makes Radix Select render its hidden native mirror, so the value
    // can be changed without driving the pointer-based popover in jsdom.
    render(
      <form>
        <PageSizeSelect {...merged} />
      </form>,
    );
    return merged;
  }

  it('shows the current page size', () => {
    renderSelect({ pageSize: 25 });
    expect(
      screen.getByRole('combobox', { name: 'Rows per page' }).textContent,
    ).toContain('25');
  });

  it('reports a new page size through onPageSizeChange', () => {
    const onPageSizeChange = vi.fn();
    renderSelect({ onPageSizeChange });
    // Drive the real control rather than a library internal: Radix mirrored
    // options into a hidden native <select>, Base UI does not.
    fireEvent.click(screen.getByRole('combobox', { name: 'Rows per page' }));
    const option = screen.getByRole('option', { name: '100' });
    // Base UI commits on the pointer sequence, not a bare click.
    fireEvent.pointerDown(option);
    fireEvent.pointerUp(option);
    fireEvent.click(option);
    expect(onPageSizeChange).toHaveBeenCalledWith(100);
  });
});

describe('CursorPager', () => {
  function renderPager(
    props: Partial<React.ComponentProps<typeof CursorPager>> = {},
  ) {
    const merged = {
      hasPrev: false,
      hasNext: false,
      onPrev: vi.fn(),
      onNext: vi.fn(),
      ...props,
    };
    render(<CursorPager {...merged} />);
    return merged;
  }

  it('disables Previous/Next according to page availability', () => {
    renderPager({ hasPrev: false, hasNext: true });
    expect(
      screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
    ).toBe(true);
    expect(
      screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled'),
    ).toBe(false);
  });

  it('fires onPrev / onNext when the buttons are clicked', () => {
    const { onPrev, onNext } = renderPager({ hasPrev: true, hasNext: true });
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(onNext).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(onPrev).toHaveBeenCalled();
  });
});
