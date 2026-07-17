// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { OrgGateFeature } from '@/org-gate/org-gate-feature';
import { useOrgGate } from '@/org-gate/use-org-gate';

import type { ApiClient } from '@pivox/client';
import type { OrgGateState, OrgGateActions } from '@/org-gate/use-org-gate';

// The gate derives its status from useOrgGate (auth + list-orgs call);
// mock it so each case drives a single status without a live API.
vi.mock('@/org-gate/use-org-gate', () => ({ useOrgGate: vi.fn() }));

const mockUseOrgGate = vi.mocked(useOrgGate);

// Only the shape the feature reads matters; the ApiClient is passed
// straight through to the (mocked) hook and never dereferenced here.
const apiClient = {} as ApiClient;

function gate(
  over: Partial<OrgGateState>,
): OrgGateState & { actions: OrgGateActions } {
  return {
    status: 'loading',
    error: null,
    actions: { retry: vi.fn() },
    ...over,
  };
}

describe('OrgGateFeature', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('redirects to /auth/create-org via the injected Navigate when the org list is empty', () => {
    mockUseOrgGate.mockReturnValue(gate({ status: 'empty' }));
    const Navigate = vi.fn(() => null);

    render(
      <OrgGateFeature apiClient={apiClient} Navigate={Navigate}>
        <div>shell</div>
      </OrgGateFeature>,
    );

    expect(Navigate).toHaveBeenCalledTimes(1);
    expect(Navigate.mock.calls[0][0]).toEqual({
      to: '/auth/create-org',
      replace: true,
    });
    expect(screen.queryByText('shell')).toBeNull();
  });

  it('renders children and does not navigate when the user has orgs', () => {
    mockUseOrgGate.mockReturnValue(gate({ status: 'ready' }));
    const Navigate = vi.fn(() => null);

    render(
      <OrgGateFeature apiClient={apiClient} Navigate={Navigate}>
        <div>shell</div>
      </OrgGateFeature>,
    );

    expect(Navigate).not.toHaveBeenCalled();
    expect(screen.queryByText('shell')).not.toBeNull();
  });

  it('renders the loading splash and does not navigate while the check is in flight', () => {
    mockUseOrgGate.mockReturnValue(gate({ status: 'loading' }));
    const Navigate = vi.fn(() => null);

    render(
      <OrgGateFeature apiClient={apiClient} Navigate={Navigate}>
        <div>shell</div>
      </OrgGateFeature>,
    );

    expect(Navigate).not.toHaveBeenCalled();
    expect(screen.queryByText('shell')).toBeNull();
    expect(screen.queryByText('Loading your organizations…')).not.toBeNull();
  });

  it('renders the error UI and does not navigate when the list call fails', () => {
    mockUseOrgGate.mockReturnValue(
      gate({ status: 'error', error: 'boom' }),
    );
    const Navigate = vi.fn(() => null);

    render(
      <OrgGateFeature apiClient={apiClient} Navigate={Navigate}>
        <div>shell</div>
      </OrgGateFeature>,
    );

    expect(Navigate).not.toHaveBeenCalled();
    expect(screen.queryByText('shell')).toBeNull();
    expect(screen.queryByText('boom')).not.toBeNull();
  });
});
