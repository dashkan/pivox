// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { RunStatus, runStatusMeta, type RunState } from '../../src/workflow/run-status';

const STATES: RunState[] = ['PENDING', 'RUNNING', 'WAITING', 'SUCCEEDED', 'FAILED', 'CANCELLED'];

describe('runStatusMeta', () => {
  it('maps all six run states to distinct labels/icons/tones', () => {
    for (const state of STATES) {
      const meta = runStatusMeta[state];
      expect(meta.label).toBeTruthy();
      expect(meta.icon).toBeDefined();
      expect(meta.tone).toBeTruthy();
      expect(meta.variant).toBeDefined();
    }
    const labels = STATES.map((s) => runStatusMeta[s].label);
    expect(new Set(labels).size).toBe(STATES.length);
  });

  it('animates only the RUNNING icon', () => {
    expect(runStatusMeta.RUNNING.spin).toBe(true);
    for (const state of STATES.filter((s) => s !== 'RUNNING')) {
      expect(runStatusMeta[state].spin).toBe(false);
    }
  });
});

describe('RunStatus', () => {
  it.each(STATES)('renders the %s badge label', (state) => {
    render(<RunStatus state={state} />);
    expect(screen.getByText(runStatusMeta[state].label)).toBeDefined();
  });
});
