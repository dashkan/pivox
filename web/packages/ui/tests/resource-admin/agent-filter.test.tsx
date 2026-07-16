// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import {
  AGENT_FILTER_ANY,
  AGENT_FILTER_CLOUD,
  AgentFilterSelect,
} from '../../src/resource-admin/agent-filter';

// Base UI combobox measures + scrolls the DOM; jsdom needs shims.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const agent = 'organizations/acme/storageGateways/gw/agents/a1';
const options = [{ value: agent, label: 'edge-01' }];

function renderAgent(value: string) {
  const onChange = vi.fn();
  render(<AgentFilterSelect value={value} options={options} onChange={onChange} />);
  return onChange;
}

const input = () => screen.getByRole('combobox') as HTMLInputElement;
const open = () => fireEvent.keyDown(input(), { key: 'ArrowDown' });

describe('AgentFilterSelect (combobox)', () => {
  it('rests on "Any" (placeholder, no selection) by default', () => {
    renderAgent(AGENT_FILTER_ANY);
    expect(input().value).toBe('');
    expect(input().placeholder).toBe('Any agent');
  });

  it('offers Cloud + each in-use agent, but not Any (Any is the empty state)', () => {
    renderAgent(AGENT_FILTER_ANY);
    open();
    expect(screen.getByText('Cloud')).toBeDefined();
    expect(screen.getByText('edge-01')).toBeDefined();
    expect(screen.queryByText('Any agent')).toBeNull();
  });

  it('shows the selected label for Cloud and for an agent', () => {
    renderAgent(AGENT_FILTER_CLOUD);
    expect(input().value).toBe('Cloud');
    renderAgent(agent);
    // Two comboboxes now mounted; the last shows the agent label.
    expect(
      (screen.getAllByRole('combobox').at(-1) as HTMLInputElement).value,
    ).toBe('edge-01');
  });

  it('picking Cloud emits the Cloud sentinel', () => {
    const onChange = renderAgent(AGENT_FILTER_ANY);
    open();
    fireEvent.click(screen.getByText('Cloud'));
    expect(onChange).toHaveBeenCalledWith(AGENT_FILTER_CLOUD);
  });

  it('picking an agent emits its resource name', () => {
    const onChange = renderAgent(AGENT_FILTER_ANY);
    open();
    fireEvent.click(screen.getByText('edge-01'));
    expect(onChange).toHaveBeenCalledWith(agent);
  });

  it('clearing returns to Any', () => {
    const onChange = renderAgent(AGENT_FILTER_CLOUD);
    fireEvent.click(
      document.querySelector('[data-slot=combobox-clear]') as HTMLElement,
    );
    expect(onChange).toHaveBeenCalledWith(AGENT_FILTER_ANY);
  });

  it('typeahead filters the agent list', () => {
    renderAgent(AGENT_FILTER_ANY);
    open();
    fireEvent.change(input(), { target: { value: 'edge' } });
    expect(screen.queryByText('Cloud')).toBeNull();
    expect(screen.getByText('edge-01')).toBeDefined();
  });

  it('shows "No agents found" when nothing matches', () => {
    renderAgent(AGENT_FILTER_ANY);
    open();
    fireEvent.change(input(), { target: { value: 'zzz' } });
    expect(screen.getByText('No agents found')).toBeDefined();
  });
});
