// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { ScopeSelect } from '../../src/resource-admin/scope-select';

import type { SpaceOption } from '../../src/resource-admin/types';

// Base UI combobox measures + scrolls the DOM; jsdom needs shims.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const spaces: SpaceOption[] = [
  { name: 'organizations/acme/spaces/main', slug: 'main', displayName: 'Main' },
  { name: 'organizations/acme/spaces/eu', slug: 'eu', displayName: 'Europe' },
];

function renderScope(
  value: string,
  props: { allLabel?: string; placeholder?: string } = {},
) {
  const onChange = vi.fn();
  render(
    <ScopeSelect
      value={value}
      spaces={spaces}
      onChange={onChange}
      allLabel={props.allLabel ?? 'Organization (no space)'}
      placeholder={props.placeholder}
    />,
  );
  return onChange;
}

const input = () => screen.getByRole('combobox') as HTMLInputElement;
const open = () => fireEvent.keyDown(input(), { key: 'ArrowDown' });

describe('ScopeSelect — create form (placeholder mode)', () => {
  it('shows the placeholder (unset) when org-direct', () => {
    renderScope('', { placeholder: 'No space — organization' });
    expect(input().value).toBe('');
    expect(input().placeholder).toBe('No space — organization');
  });

  it('shows the space name when a space is selected', () => {
    renderScope('main', { placeholder: 'No space — organization' });
    expect(input().value).toBe('Main');
  });

  it('picking a space calls onChange(slug)', () => {
    const onChange = renderScope('', { placeholder: 'No space — organization' });
    open();
    fireEvent.click(screen.getByText('Europe'));
    expect(onChange).toHaveBeenCalledWith('eu');
  });

  it('typeahead filters the space list', () => {
    renderScope('', { placeholder: 'No space — organization' });
    open();
    expect(screen.getByText('Main')).toBeDefined();
    fireEvent.change(input(), { target: { value: 'eur' } });
    expect(screen.queryByText('Main')).toBeNull();
    expect(screen.getByText('Europe')).toBeDefined();
  });

  it('shows "No spaces found" when the query matches nothing', () => {
    renderScope('', { placeholder: 'No space — organization' });
    open();
    fireEvent.change(input(), { target: { value: 'zzz' } });
    expect(screen.getByText('No spaces found')).toBeDefined();
  });

  it('clearing sets scope back to empty', () => {
    const onChange = renderScope('main', {
      placeholder: 'No space — organization',
    });
    fireEvent.click(
      document.querySelector('[data-slot=combobox-clear]') as HTMLElement,
    );
    expect(onChange).toHaveBeenCalledWith('');
  });
});

describe('ScopeSelect — filter (no placeholder)', () => {
  it('shows allLabel as the resting label for the empty scope', () => {
    renderScope('', { allLabel: 'All spaces' });
    expect(input().placeholder).toBe('All spaces');
  });
});

// The popup-portal-container tests lived here. They covered `container` /
// `positionMethod`, which existed only to escape Radix Dialog's body-level
// pointer-lock. Base UI scopes modality to an internal backdrop, so that
// plumbing was removed from ScopeSelect and these tests with it.
