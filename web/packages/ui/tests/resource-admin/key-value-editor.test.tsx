// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { KeyValueEditor } from '../../src/resource-admin/key-value-editor';

import type { Suggestion } from '../../src/resource-admin/suggest-combobox';
import type { KeyValueEntry } from '../../src/resource-admin/types';

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const headers: Suggestion[] = [
  { name: 'Authorization', description: 'Credentials for the target API' },
];

function renderEditor(props: {
  entries: KeyValueEntry[];
  keySuggestions?: Suggestion[];
}) {
  const onChange = vi.fn();
  render(
    <KeyValueEditor
      label="Headers"
      keyPlaceholder="Authorization"
      valuePlaceholder="value"
      entries={props.entries}
      onChange={onChange}
      keySuggestions={props.keySuggestions}
    />,
  );
  return onChange;
}

describe('KeyValueEditor — key field', () => {
  it('renders a plain input for the key when no suggestions are given', () => {
    renderEditor({ entries: [] });
    // No combobox; the key field is a labeled text input.
    expect(screen.queryByRole('combobox')).toBeNull();
    expect(screen.getByLabelText('Headers name 1')).toBeDefined();
  });

  it('renders a creatable combobox for the key when suggestions are given', () => {
    renderEditor({ entries: [], keySuggestions: headers });
    // The (trailing) key row is now a combobox with the suggestions.
    const combobox = screen.getByRole('combobox');
    expect(combobox).toBeDefined();
    fireEvent.keyDown(combobox, { key: 'ArrowDown' });
    expect(screen.getByText('Authorization')).toBeDefined();
  });

  it('commits a custom key by selecting its raw-value option', () => {
    const onChange = renderEditor({ entries: [], keySuggestions: headers });
    const combobox = screen.getByRole('combobox');
    // Type a custom name, then select the surfaced raw-value option.
    fireEvent.change(combobox, { target: { value: 'X-Trace-Id' } });
    fireEvent.keyDown(combobox, { key: 'ArrowDown' });
    fireEvent.click(screen.getByText('X-Trace-Id'));
    // The trailing sentinel row's key becomes the selected value.
    expect(onChange).toHaveBeenLastCalledWith([
      { key: 'X-Trace-Id', value: '' },
    ]);
  });
});
