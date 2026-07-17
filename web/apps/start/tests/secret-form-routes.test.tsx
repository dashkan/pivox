// @vitest-environment jsdom
import {
  SecretCreateFeature,
  SecretEditFeature,
} from '@pivox/features/secrets';
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
// invokes them; the sanitizer + fallback itself is covered in return-to.test.ts.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

afterEach(cleanup);

const SPACES_PATH = '/v1/organizations/{organization}/spaces';
const SECRET_PATH = '/v1/organizations/{organization}/secrets/{secret}';
const SPACE_SECRET_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}';

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

describe('SecretCreateFeature (routed create page)', () => {
  it('renders the create form with the standard title + submit', () => {
    render(
      <SecretCreateFeature
        $api={fakeApi({ [SPACES_PATH]: { spaces: [] } })}
        apiClient={makeApiClient()}
        parent={parent}
        back={<a href="/secrets">← Secrets</a>}
        onCancel={() => {}}
        onSubmitSuccess={() => {}}
      />,
    );
    expect(screen.getByRole('heading', { name: 'New secret' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Create secret' })).toBeDefined();
  });

  it('navigates back (onSubmitSuccess) after a successful create', async () => {
    const onSubmitSuccess = vi.fn();
    const apiClient = makeApiClient();
    render(
      <SecretCreateFeature
        $api={fakeApi({ [SPACES_PATH]: { spaces: [] } })}
        apiClient={apiClient}
        parent={parent}
        back={<a href="/secrets">← Secrets</a>}
        onCancel={() => {}}
        onSubmitSuccess={onSubmitSuccess}
      />,
    );

    fireEvent.change(screen.getAllByRole('textbox')[0], {
      target: { value: 'Stripe key' },
    });
    fireEvent.change(document.querySelector('#secret-value') as HTMLInputElement, {
      target: { value: 's3cr3t' },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Create secret' }));
    });

    expect(apiClient.POST).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(onSubmitSuccess).toHaveBeenCalledTimes(1));
  });

  it('navigates back (onCancel) when Cancel is clicked on a clean form', () => {
    const onCancel = vi.fn();
    render(
      <SecretCreateFeature
        $api={fakeApi({ [SPACES_PATH]: { spaces: [] } })}
        apiClient={makeApiClient()}
        parent={parent}
        back={<a href="/secrets">← Secrets</a>}
        onCancel={onCancel}
        onSubmitSuccess={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});

describe('SecretEditFeature (routed edit page)', () => {
  const record = {
    name: 'organizations/acme/spaces/main/secrets/vizrt-key',
    displayName: 'VizRT key',
    etag: 'e1',
  };

  function renderEdit(onSubmitSuccess = () => {}) {
    render(
      <SecretEditFeature
        $api={fakeApi({
          [SPACES_PATH]: {
            spaces: [
              { name: 'organizations/acme/spaces/main', displayName: 'Main' },
            ],
          },
          [SPACE_SECRET_PATH]: record,
          [SECRET_PATH]: record,
        })}
        apiClient={makeApiClient()}
        parent={parent}
        secretId="vizrt-key"
        space="main"
        back={<a href="/secrets">← Secrets</a>}
        onCancel={() => {}}
        onSubmitSuccess={onSubmitSuccess}
      />,
    );
  }

  it('seeds the edit form from the SSR-primed record (metadata only)', () => {
    renderEdit();
    expect(screen.getByRole('heading', { name: 'Edit secret' })).toBeDefined();
    expect(screen.getByDisplayValue('VizRT key')).toBeDefined();
    // Set-only: no value field until Rotate is ticked.
    expect(document.querySelector('input[type="password"]')).toBeNull();
    // Delete-on-edit is present (composition), create's isn't.
    expect(screen.getByRole('button', { name: 'Delete secret' })).toBeDefined();
  });

  it('navigates back (onSubmitSuccess) after a successful metadata save', async () => {
    const onSubmitSuccess = vi.fn();
    renderEdit(onSubmitSuccess);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    });
    await waitFor(() => expect(onSubmitSuccess).toHaveBeenCalledTimes(1));
  });
});
