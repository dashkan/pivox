// @vitest-environment jsdom
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { AdminSearch } from '../../src/resource-admin/admin-search';

afterEach(() => {
  vi.useRealTimers();
});

describe('AdminSearch', () => {
  it('reports each keystroke to onChange when not debounced', () => {
    const onChange = vi.fn();
    render(<AdminSearch value="" onChange={onChange} placeholder="Search…" />);
    fireEvent.change(screen.getByRole('searchbox'), {
      target: { value: 'stripe' },
    });
    expect(onChange).toHaveBeenCalledWith('stripe');
  });

  it('shows keystrokes immediately but commits onChange only after the debounce', () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    render(<AdminSearch value="" onChange={onChange} debounceMs={300} />);
    const box = screen.getByRole('searchbox');

    fireEvent.change(box, { target: { value: 'st' } });
    fireEvent.change(box, { target: { value: 'stripe' } });
    // The input reflects the draft immediately; onChange has not fired yet.
    expect(box).toHaveProperty('value', 'stripe');
    expect(onChange).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(300);
    });
    // Only the latest value is committed, once.
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('stripe');
  });

  it('resyncs the draft when the committed value changes from outside', () => {
    const { rerender } = render(<AdminSearch value="hub" onChange={vi.fn()} />);
    expect(screen.getByRole('searchbox')).toHaveProperty('value', 'hub');
    rerender(<AdminSearch value="" onChange={vi.fn()} />);
    expect(screen.getByRole('searchbox')).toHaveProperty('value', '');
  });

  it('cancels a pending debounce when the value is cleared from outside', () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    const { rerender } = render(
      <AdminSearch value="ab" onChange={onChange} debounceMs={300} />,
    );
    // Type within the debounce window, then Clear filters resets the committed
    // value before the timer fires.
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'abc' } });
    rerender(<AdminSearch value="" onChange={onChange} debounceMs={300} />);

    act(() => {
      vi.advanceTimersByTime(300);
    });
    // The stale "abc" timer must NOT re-commit the filter that was just cleared.
    expect(onChange).not.toHaveBeenCalledWith('abc');
    expect(screen.getByRole('searchbox')).toHaveProperty('value', '');
  });
});
