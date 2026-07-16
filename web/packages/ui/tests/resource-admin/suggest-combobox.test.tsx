// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { SuggestCombobox } from '../../src/resource-admin/suggest-combobox';

import type { Suggestion } from '../../src/resource-admin/suggest-combobox';

// Base UI combobox measures + scrolls the DOM; jsdom needs shims.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const suggestions: Suggestion[] = [
  { name: 'Authorization', description: 'Credentials for the target API' },
  { name: 'Content-Type', description: 'Media type of the request body' },
];

// Controlled `inputValue` needs a stateful parent to reflect typed text.
function Stateful({ onCommit }: { onCommit: (v: string) => void }) {
  const [value, setValue] = useState('');
  return (
    <SuggestCombobox
      value={value}
      onChange={(v) => {
        setValue(v);
        onCommit(v);
      }}
      suggestions={suggestions}
      placeholder="Authorization"
    />
  );
}

const input = () => screen.getByRole('combobox') as HTMLInputElement;
const open = () => fireEvent.keyDown(input(), { key: 'ArrowDown' });

describe('SuggestCombobox (creatable)', () => {
  it('renders suggestions with their descriptions', () => {
    render(<Stateful onCommit={vi.fn()} />);
    open();
    expect(screen.getByText('Authorization')).toBeDefined();
    expect(screen.getByText('Credentials for the target API')).toBeDefined();
    expect(screen.getByText('Content-Type')).toBeDefined();
  });

  it('surfaces a custom typed value as a plain (raw-label) option and commits it', () => {
    const onCommit = vi.fn();
    render(<Stateful onCommit={onCommit} />);
    fireEvent.change(input(), { target: { value: 'X-Custom-Header' } });
    open();
    // The option reads the raw text — no "Create …" wording.
    expect(screen.getByText('X-Custom-Header')).toBeDefined();
    expect(screen.queryByText(/Create "/)).toBeNull();
    fireEvent.click(screen.getByText('X-Custom-Header'));
    expect(onCommit).toHaveBeenLastCalledWith('X-Custom-Header');
    // No confirmation dialog — inline commit.
    expect(document.querySelector('[role=dialog]')).toBeNull();
  });

  it('selecting a suggestion commits its name', () => {
    const onCommit = vi.fn();
    render(<Stateful onCommit={onCommit} />);
    fireEvent.change(input(), { target: { value: 'auth' } });
    open();
    fireEvent.click(screen.getByText('Authorization'));
    expect(onCommit).toHaveBeenLastCalledWith('Authorization');
  });

  it('does not offer a duplicate custom item when the query matches a suggestion exactly', () => {
    render(<Stateful onCommit={vi.fn()} />);
    fireEvent.change(input(), { target: { value: 'Authorization' } });
    open();
    // Exactly one "Authorization" option (no synthetic duplicate).
    expect(screen.getAllByText('Authorization')).toHaveLength(1);
  });

  it('shows the current value in the input', () => {
    render(
      <SuggestCombobox
        value="X-Api-Key"
        onChange={vi.fn()}
        suggestions={suggestions}
      />,
    );
    expect(input().value).toBe('X-Api-Key');
  });

  it('shows the FULL suggestion list even when the input equals a committed value', () => {
    // Committed to a suggestion → reopening must not narrow to just that one.
    render(
      <SuggestCombobox
        value="Authorization"
        onChange={vi.fn()}
        suggestions={suggestions}
      />,
    );
    open();
    expect(screen.getByText('Authorization')).toBeDefined();
    expect(screen.getByText('Content-Type')).toBeDefined();
    // And no self-duplicate synthetic for the already-committed value.
    expect(screen.getAllByText('Authorization')).toHaveLength(1);
  });

  it('keeps the committed value visible in the input when opened (does not clear it)', () => {
    render(
      <SuggestCombobox
        value="Authorization"
        onChange={vi.fn()}
        suggestions={suggestions}
      />,
    );
    open();
    // Opening shows the full list AND leaves the committed value on display.
    expect(input().value).toBe('Authorization');
    expect(screen.getByText('Content-Type')).toBeDefined();
  });

  it('describes a custom typed value like a suggestion', () => {
    render(<Stateful onCommit={vi.fn()} />);
    fireEvent.change(input(), { target: { value: 'X-Trace-Id' } });
    open();
    expect(screen.getByText('X-Trace-Id')).toBeDefined();
    expect(screen.getByText('Custom header')).toBeDefined();
  });
});
