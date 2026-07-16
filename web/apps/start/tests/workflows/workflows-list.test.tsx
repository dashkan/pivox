// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { vi } from 'vitest';

import type { ReactNode } from 'react';
import type { ReactQueryApi } from '@pivox/client/react-query';

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}));

import { WorkflowsList } from '../../src/routes/_app/workflows/index';

function apiReturning(result: unknown): ReactQueryApi {
  return { useQuery: () => result } as unknown as ReactQueryApi;
}

const parent = 'organizations/acme';

describe('WorkflowsList', () => {
  it('renders a row per workflow with origin, enabled and version', () => {
    const $api = apiReturning({
      isLoading: false,
      error: undefined,
      data: {
        workflows: [
          {
            name: 'organizations/acme/workflows/ingest',
            displayName: 'Ingest',
            origin: 'OWNED',
            enabled: true,
            version: 'organizations/acme/workflows/ingest/versions/2',
            updateTime: '2026-01-01T00:00:00Z',
          },
          {
            name: 'organizations/acme/workflows/sys',
            displayName: 'System',
            origin: 'MANAGED',
            enabled: false,
          },
        ],
      },
    });

    render(<WorkflowsList $api={$api} parent={parent} />);

    expect(screen.getByText('Ingest')).toBeDefined();
    expect(screen.getByText('System')).toBeDefined();
    expect(screen.getByText('Owned')).toBeDefined();
    expect(screen.getByText('Managed')).toBeDefined();
    // "Enabled" is also a column header, so the cell is one of several matches.
    expect(screen.getAllByText('Enabled').length).toBeGreaterThan(0);
    expect(screen.getByText('Disabled')).toBeDefined();
    expect(screen.getByText('2')).toBeDefined();
  });

  it('shows an empty notice when there are no workflows', () => {
    const $api = apiReturning({ isLoading: false, error: undefined, data: { workflows: [] } });
    render(<WorkflowsList $api={$api} parent={parent} />);
    expect(screen.getByText('No workflows yet.')).toBeDefined();
  });

  it('shows a loading notice while fetching', () => {
    const $api = apiReturning({ isLoading: true, error: undefined, data: undefined });
    render(<WorkflowsList $api={$api} parent={parent} />);
    expect(screen.getByText('Loading workflows…')).toBeDefined();
  });
});
