// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { FilterToggleButton } from '../../src/resource-admin/filter-toggle-button';

describe('FilterToggleButton', () => {
  it('reflects the active state via aria-pressed', () => {
    const { rerender } = render(
      <FilterToggleButton active={false} onToggle={vi.fn()} />,
    );
    expect(
      screen.getByRole('button', { name: 'Filter' }).getAttribute('aria-pressed'),
    ).toBe('false');

    rerender(<FilterToggleButton active={true} onToggle={vi.fn()} />);
    expect(
      screen.getByRole('button', { name: 'Filter' }).getAttribute('aria-pressed'),
    ).toBe('true');
  });

  it('fires onToggle on click', () => {
    const onToggle = vi.fn();
    render(<FilterToggleButton active={false} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    expect(onToggle).toHaveBeenCalled();
  });
});
