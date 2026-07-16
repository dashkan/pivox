// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { AgentSelect } from '../../src/resource-admin/agent-select';

import type { AgentOption } from '../../src/resource-admin/types';

// Base UI combobox measures + scrolls the DOM; jsdom needs shims.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const options: AgentOption[] = [
  { value: 'organizations/acme/storageGateways/gw1/agents/a1', label: 'edge-01' },
  { value: 'organizations/acme/storageGateways/gw2/agents/a2', label: 'edge-02' },
];

const input = () => screen.getByRole('combobox') as HTMLInputElement;
const open = () => fireEvent.keyDown(input(), { key: 'ArrowDown' });

describe('AgentSelect (combobox)', () => {
  it('rests on "None (runs in cloud)" when value is empty', () => {
    render(<AgentSelect value="" options={options} onChange={vi.fn()} />);
    expect(input().value).toBe('');
    expect(input().placeholder).toBe('None (runs in cloud)');
  });

  it('shows the empty placeholder when there are no agents', () => {
    render(<AgentSelect value="" options={[]} onChange={vi.fn()} />);
    expect(input().placeholder).toBe('No agents — runs in cloud');
  });

  it('renders the selected agent label', () => {
    render(<AgentSelect value={options[0].value} options={options} onChange={vi.fn()} />);
    expect(input().value).toBe('edge-01');
  });

  it('picking an agent emits its resource name', () => {
    const onChange = vi.fn();
    render(<AgentSelect value="" options={options} onChange={onChange} />);
    open();
    fireEvent.click(screen.getByText('edge-02'));
    expect(onChange).toHaveBeenCalledWith(options[1].value);
  });

  it('clearing returns to cloud (empty value)', () => {
    const onChange = vi.fn();
    render(<AgentSelect value={options[0].value} options={options} onChange={onChange} />);
    fireEvent.click(
      document.querySelector('[data-slot=combobox-clear]') as HTMLElement,
    );
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('keeps an editing connector’s agent selectable even if not in the list', () => {
    const missing = 'organizations/acme/storageGateways/gw9/agents/a9';
    render(<AgentSelect value={missing} options={options} onChange={vi.fn()} />);
    // Falls back to the resource-name leaf as the label.
    expect(input().value).toBe('a9');
  });
});
