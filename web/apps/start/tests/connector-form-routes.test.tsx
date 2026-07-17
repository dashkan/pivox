// @vitest-environment jsdom
import {
  ConnectorCreateFeature,
  ConnectorEditFeature,
} from '@pivox/features/connectors';
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';

// The routed create/edit pages inject `onCancel` / `onSubmitSuccess` — both are
// the route's `router.history.push(returnTo)`. These tests prove the FORM wiring
// invokes them (so navigation to the sanitized `?from=` fires); the sanitizer +
// fallback itself is covered in return-to.test.ts.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

// Start's vitest config doesn't auto-clean between tests; do it explicitly so
// repeated renders don't collide ("found multiple elements").
afterEach(cleanup);

const SPACES_PATH = '/v1/organizations/{organization}/spaces';
const CONNECTOR_PATH = '/v1/organizations/{organization}/connectors/{connector}';
const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}';

/** A fake `$api` that returns canned data per path and honors `enabled: false`. */
function fakeApi(dataByPath: Record<string, unknown>): ReactQueryApi {
  return {
    useQuery: (
      _method: string,
      path: string,
      _init: unknown,
      options?: { enabled?: boolean },
    ) => {
      if (options && options.enabled === false) {
        return { data: undefined, isLoading: false, error: undefined };
      }
      return { data: dataByPath[path], isLoading: false, error: undefined };
    },
  } as unknown as ReactQueryApi;
}

function makeApiClient() {
  return {
    GET: vi.fn(() => Promise.resolve({ data: undefined, error: undefined })),
    POST: vi.fn(() => Promise.resolve({ error: undefined })),
    PATCH: vi.fn(() => Promise.resolve({ error: undefined })),
    DELETE: vi.fn(() => Promise.resolve({ error: undefined })),
  } as unknown as ApiClient;
}

const parent = 'organizations/acme';

describe('ConnectorCreateFeature (routed create page)', () => {
  it('renders the create form with the standard title + submit', () => {
    render(
      <ConnectorCreateFeature
        $api={fakeApi({ [SPACES_PATH]: { spaces: [] } })}
        apiClient={makeApiClient()}
        parent={parent}
        agentOptions={[]}
        back={<a href="/connectors">← Connectors</a>}
        onCancel={() => {}}
        onSubmitSuccess={() => {}}
      />,
    );
    expect(screen.getByRole('heading', { name: 'New connector' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Create connector' })).toBeDefined();
  });

  it('navigates back (onSubmitSuccess) after a successful create', async () => {
    const onSubmitSuccess = vi.fn();
    const apiClient = makeApiClient();
    render(
      <ConnectorCreateFeature
        $api={fakeApi({ [SPACES_PATH]: { spaces: [] } })}
        apiClient={apiClient}
        parent={parent}
        agentOptions={[]}
        back={<a href="/connectors">← Connectors</a>}
        onCancel={() => {}}
        onSubmitSuccess={onSubmitSuccess}
      />,
    );

    fireEvent.change(screen.getAllByRole('textbox')[0], {
      target: { value: 'Stripe' },
    });
    fireEvent.change(screen.getByPlaceholderText('https://api.example.com'), {
      target: { value: 'https://api.stripe.com' },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Create connector' }));
    });

    expect(apiClient.POST).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(onSubmitSuccess).toHaveBeenCalledTimes(1));
  });

  it('navigates back (onCancel) when Cancel is clicked on a clean form', () => {
    const onCancel = vi.fn();
    render(
      <ConnectorCreateFeature
        $api={fakeApi({ [SPACES_PATH]: { spaces: [] } })}
        apiClient={makeApiClient()}
        parent={parent}
        agentOptions={[]}
        back={<a href="/connectors">← Connectors</a>}
        onCancel={onCancel}
        onSubmitSuccess={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});

describe('ConnectorEditFeature (routed edit page)', () => {
  const record = {
    name: 'organizations/acme/spaces/main/connectors/vizrt',
    displayName: 'VizRT',
    http: { baseUrl: 'https://vizrt.example.com' },
  };

  function renderEdit(onSubmitSuccess = () => {}) {
    render(
      <ConnectorEditFeature
        $api={fakeApi({
          [SPACES_PATH]: {
            spaces: [
              {
                name: 'organizations/acme/spaces/main',
                displayName: 'Main',
              },
            ],
          },
          [SPACE_CONNECTOR_PATH]: record,
          [CONNECTOR_PATH]: record,
        })}
        apiClient={makeApiClient()}
        parent={parent}
        connectorId="vizrt"
        space="main"
        agentOptions={[]}
        back={<a href="/connectors">← Connectors</a>}
        onCancel={() => {}}
        onSubmitSuccess={onSubmitSuccess}
      />,
    );
  }

  it('seeds the edit form from the SSR-primed record', () => {
    renderEdit();
    expect(screen.getByRole('heading', { name: 'Edit connector' })).toBeDefined();
    expect(screen.getByDisplayValue('VizRT')).toBeDefined();
    expect(screen.getByDisplayValue('https://vizrt.example.com')).toBeDefined();
    // Delete-on-edit is present (composition), create's isn't.
    expect(screen.getByRole('button', { name: 'Delete connector' })).toBeDefined();
  });

  it('navigates back (onSubmitSuccess) after a successful save', async () => {
    const onSubmitSuccess = vi.fn();
    renderEdit(onSubmitSuccess);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    });
    await waitFor(() => expect(onSubmitSuccess).toHaveBeenCalledTimes(1));
  });
});
