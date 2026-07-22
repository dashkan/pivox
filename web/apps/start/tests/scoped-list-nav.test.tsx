// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  cleanup,
  fireEvent,
  render,
  screen,
} from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

// Mock the router's navigation hooks so we can assert the scope-in-URL shells
// resolve create/edit to the RIGHT scoped route (params + `to`). This is the
// wiring guard the task requires: a pure-logic test alone would not catch a link
// pointing at a dead route. `useRouterState` feeds the `?from=` return href.
const navigate = vi.fn();
const routerPush = vi.fn();
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
  useRouter: () => ({ history: { push: routerPush } }),
  useRouterState: (opts: { select: (s: unknown) => unknown }) =>
    opts.select({
      location: { pathname: '/organizations/acme/secrets', searchStr: '' },
    }),
}));

import { buildSecretsListRequest } from '@pivox/features/secrets';
import { buildWorkflowsListRequest } from '@pivox/features/workflows';

import { ScopedSecretsList } from '../src/features/secrets/scoped-secrets-list';
import { ScopedWorkflowsList } from '../src/features/workflows/scoped-workflows-list';
import { ScopedWorkflowDetail } from '../src/features/workflows/scoped-workflow-detail';
import { $api } from '../src/lib/api-client';
import { searchToValue as secretsSearchToValue } from '../src/lib/secrets-search';
import { searchToValue as workflowsSearchToValue } from '../src/lib/workflows-search';

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

afterEach(() => {
  navigate.mockClear();
  routerPush.mockClear();
  cleanup();
});

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;
const SPACE_SECRETS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;
const WORKFLOWS_PATH = '/v1/organizations/{organization}/workflows' as const;
const SPACE_WORKFLOWS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/workflows' as const;

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
}

type SecretRow = { name: string; displayName: string };

// Prime under the SAME react-query key the list hook produces — built via the
// shared `buildSecretsListRequest` (byte-identical query), exactly as the route
// loader + secrets-ssr test do. A key mismatch would render an empty list.
function primeOrgSecrets(qc: QueryClient, secrets: SecretRow[]) {
  const req = buildSecretsListRequest('acme', {
    ...secretsSearchToValue({}),
    scope: '',
  });
  const { queryKey } = $api.queryOptions('get', SECRETS_PATH, {
    params: { path: { organization: 'acme' }, query: req.query },
  });
  qc.setQueryData(queryKey, { secrets });
}

function primeSpaceSecrets(qc: QueryClient, secrets: SecretRow[]) {
  const req = buildSecretsListRequest('acme', {
    ...secretsSearchToValue({}),
    scope: 'dev',
  });
  if (!req.isSpaceScoped) throw new Error('expected space-scoped request');
  const { queryKey } = $api.queryOptions('get', SPACE_SECRETS_PATH, {
    params: { path: req.pathParams, query: req.query },
  });
  qc.setQueryData(queryKey, { secrets });
}

describe('ScopedSecretsList — create navigation', () => {
  it('org rollup: New secret → the org-scoped create route', () => {
    const qc = newClient();
    primeOrgSecrets(qc, []);
    render(
      <QueryClientProvider client={qc}>
        <ScopedSecretsList orgSlug="acme" search={{}} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'New secret' }));
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: '/organizations/$organization/secrets/new',
        params: { organization: 'acme' },
        search: { from: '/organizations/acme/secrets' },
      }),
    );
  });

  it('space scope: New secret → the space-scoped create route', () => {
    const qc = newClient();
    primeSpaceSecrets(qc, []);
    render(
      <QueryClientProvider client={qc}>
        <ScopedSecretsList orgSlug="acme" spaceSlug="dev" search={{}} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'New secret' }));
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: '/organizations/$organization/spaces/$space/secrets/new',
        params: { organization: 'acme', space: 'dev' },
      }),
    );
  });
});

describe('ScopedSecretsList — edit navigation targets the secret’s OWN scope', () => {
  it('org-direct row → the org-scoped edit route', () => {
    const qc = newClient();
    primeOrgSecrets(qc, [
      { name: 'organizations/acme/secrets/s1', displayName: 'S1' },
    ]);
    render(
      <QueryClientProvider client={qc}>
        <ScopedSecretsList orgSlug="acme" search={{}} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Edit secret' }));
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: '/organizations/$organization/secrets/$secretId/edit',
        params: { organization: 'acme', secretId: 's1' },
      }),
    );
  });

  it('space-scoped row (in the org rollup) → the space-scoped edit route', () => {
    const qc = newClient();
    primeOrgSecrets(qc, [
      { name: 'organizations/acme/spaces/dev/secrets/s2', displayName: 'S2' },
    ]);
    render(
      <QueryClientProvider client={qc}>
        <ScopedSecretsList orgSlug="acme" search={{}} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Edit secret' }));
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: '/organizations/$organization/spaces/$space/secrets/$secretId/edit',
        params: { organization: 'acme', space: 'dev', secretId: 's2' },
      }),
    );
  });
});

type WorkflowRow = { name: string; displayName: string };

// Prime under the SAME react-query key the workflows list hook produces — built
// via the shared `buildWorkflowsListRequest`, byte-identical to the route loader.
function primeOrgWorkflows(qc: QueryClient, workflows: WorkflowRow[]) {
  const req = buildWorkflowsListRequest('acme', {
    ...workflowsSearchToValue({}),
    scope: '',
  });
  const { queryKey } = $api.queryOptions('get', WORKFLOWS_PATH, {
    params: { path: { organization: 'acme' }, query: req.query },
  });
  qc.setQueryData(queryKey, { workflows });
}

function primeSpaceWorkflows(qc: QueryClient, workflows: WorkflowRow[]) {
  const req = buildWorkflowsListRequest('acme', {
    ...workflowsSearchToValue({}),
    scope: 'dev',
  });
  if (!req.isSpaceScoped) throw new Error('expected space-scoped request');
  const { queryKey } = $api.queryOptions('get', SPACE_WORKFLOWS_PATH, {
    params: { path: req.pathParams, query: req.query },
  });
  qc.setQueryData(queryKey, { workflows });
}

describe('ScopedWorkflowsList — row navigates to the scoped canvas detail', () => {
  it('org rollup: clicking a workflow row → the org-scoped detail route', () => {
    const qc = newClient();
    primeOrgWorkflows(qc, [
      { name: 'organizations/acme/workflows/ingest', displayName: 'Ingest' },
    ]);
    render(
      <QueryClientProvider client={qc}>
        <ScopedWorkflowsList orgSlug="acme" search={{}} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Ingest' }));
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: '/organizations/$organization/workflows/$workflowId',
        params: { organization: 'acme', workflowId: 'ingest' },
      }),
    );
  });

  it('space scope: clicking a workflow row → the space-scoped detail route', () => {
    const qc = newClient();
    primeSpaceWorkflows(qc, [
      {
        name: 'organizations/acme/spaces/dev/workflows/ingest',
        displayName: 'Ingest',
      },
    ]);
    render(
      <QueryClientProvider client={qc}>
        <ScopedWorkflowsList orgSlug="acme" spaceSlug="dev" search={{}} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Ingest' }));
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: '/organizations/$organization/spaces/$space/workflows/$workflowId',
        params: { organization: 'acme', space: 'dev', workflowId: 'ingest' },
      }),
    );
  });
});

describe('ScopedWorkflowDetail — back link resolves to the scoped list', () => {
  it('org scope: ← Workflows soft-navigates to the org workflows list', () => {
    const qc = newClient();
    render(
      <QueryClientProvider client={qc}>
        <ScopedWorkflowDetail orgSlug="acme" workflowId="ingest" />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('link', { name: '← Workflows' }));
    expect(routerPush).toHaveBeenCalledWith('/organizations/acme/workflows');
  });

  it('space scope: ← Workflows soft-navigates to the space workflows list', () => {
    const qc = newClient();
    render(
      <QueryClientProvider client={qc}>
        <ScopedWorkflowDetail
          orgSlug="acme"
          spaceSlug="dev"
          workflowId="ingest"
        />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('link', { name: '← Workflows' }));
    expect(routerPush).toHaveBeenCalledWith(
      '/organizations/acme/spaces/dev/workflows',
    );
  });
});
