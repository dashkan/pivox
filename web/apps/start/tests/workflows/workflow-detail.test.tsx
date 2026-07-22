// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { ReactNode } from 'react';
import type { ApiClient, components } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}));

const useWorkflowDefinition = vi.fn();
vi.mock('@pivox/features/workflows', () => ({
  useWorkflowDefinition: (arg: unknown) => useWorkflowDefinition(arg),
}));

vi.mock('@pivox/ui/workflow', () => ({
  WorkflowCanvas: () => <div data-testid="workflow-canvas" />,
}));

import { WorkflowDetail } from '../../src/features/workflows/scoped-workflow-detail';

type Workflow = components['schemas']['v1Workflow'];

function apiWithWorkflow(workflow: Workflow | undefined, loading = false): ReactQueryApi {
  return {
    useQuery: () => ({ data: workflow, isLoading: loading, error: undefined, refetch: vi.fn() }),
  } as unknown as ReactQueryApi;
}

const apiClient = {} as ApiClient;
const parent = 'organizations/acme';

describe('WorkflowDetail', () => {
  it('renders the four tabs and mounts the canvas on the Definition tab', () => {
    useWorkflowDefinition.mockReturnValue({
      graph: { nodes: [], edges: [] },
      isLoading: false,
      error: null,
    });

    const workflow: Workflow = {
      name: 'organizations/acme/workflows/ingest',
      displayName: 'Ingest',
      origin: 'OWNED',
      enabled: true,
      version: 'organizations/acme/workflows/ingest/versions/2',
    };

    render(
      <WorkflowDetail
        $api={apiWithWorkflow(workflow)}
        apiClient={apiClient}
        parent={parent}
        workflowId="ingest"
        back={<a href="#">← Workflows</a>}
      />,
    );

    expect(screen.getByRole('tab', { name: 'Definition' })).toBeDefined();
    expect(screen.getByRole('tab', { name: 'Versions' })).toBeDefined();
    expect(screen.getByRole('tab', { name: 'Settings' })).toBeDefined();
    expect(screen.getByRole('tab', { name: 'Runs' })).toBeDefined();

    // Definition is the default tab; its canvas mounts with the live version.
    expect(screen.getByTestId('workflow-canvas')).toBeDefined();
    expect(useWorkflowDefinition).toHaveBeenCalledWith(
      expect.objectContaining({ name: workflow.version }),
    );
  });

  it('shows a no-version notice when the workflow has no promoted version', () => {
    const workflow: Workflow = {
      name: 'organizations/acme/workflows/draft',
      displayName: 'Draft',
      origin: 'OWNED',
      version: '',
    };

    render(
      <WorkflowDetail
        $api={apiWithWorkflow(workflow)}
        apiClient={apiClient}
        parent={parent}
        workflowId="draft"
        back={<a href="#">← Workflows</a>}
      />,
    );

    expect(screen.getByText('This workflow has no promoted version yet.')).toBeDefined();
  });
});
