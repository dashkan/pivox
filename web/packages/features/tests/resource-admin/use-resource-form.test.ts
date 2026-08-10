// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { useResourceForm } from '@/resource-admin/use-resource-form';

import type { FormDescriptor, RecordQueryState } from '@/resource-admin/form-descriptor';
import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { FormMode } from '@pivox/ui/form-page';

/**
 * Proves the generic create/edit FORM hook is resource-agnostic: it drives an
 * arbitrary `Widget` through the SSR-primed record load, the create/update save
 * (success + HTTP-error + transport-throw), and the delete-confirm — all via the
 * descriptor's pure async methods.
 */

interface Widget {
  id: string;
}
interface WidgetValues {
  name: string;
}

let recordResult: RecordQueryState<Widget>;
let lastUseRecordInput: { enabled: boolean; id?: string; space?: string } | null =
  null;
const saveSpy = vi.fn(() => Promise.resolve({ error: undefined }));
const removeSpy = vi.fn(() => Promise.resolve({ error: undefined }));

function makeDescriptor(): FormDescriptor<Widget, WidgetValues> {
  return {
    useRecord: ({ id, space, enabled }) => {
      lastUseRecordInput = { enabled, id, space };
      return recordResult;
    },
    save: saveSpy,
    remove: removeSpy,
    loadErrorFallback: 'widget load failed',
    saveErrorFallback: 'widget save failed',
  };
}

function render(opts: {
  mode: FormMode;
  id?: string;
  space?: string;
  record?: Partial<RecordQueryState<Widget>>;
}) {
  recordResult = {
    data: undefined,
    isLoading: false,
    error: undefined,
    ...opts.record,
  };
  const onDone = vi.fn();
  const apiClient = {} as ApiClient;
  const $api = {} as ReactQueryApi;
  const descriptor = makeDescriptor();
  const { result } = renderHook(() =>
    useResourceForm(descriptor, {
      $api,
      apiClient,
      parent: 'organizations/acme',
      mode: opts.mode,
      id: opts.id,
      space: opts.space,
      onDone,
    }),
  );
  return { result, onDone, apiClient };
}

async function settle() {
  await act(async () => {});
}

describe('useResourceForm — record load', () => {
  it('enables the detail query only in edit mode with an id', () => {
    render({ mode: 'edit', id: 'w1' });
    expect(lastUseRecordInput).toMatchObject({ enabled: true, id: 'w1' });
  });

  it('keeps the detail query inert in create mode', () => {
    const { result } = render({ mode: 'create' });
    expect(lastUseRecordInput?.enabled).toBe(false);
    expect(result.current.record).toBeNull();
    expect(result.current.recordLoading).toBe(false);
    expect(result.current.onDelete).toBeUndefined();
  });

  it('exposes the loaded record + a load error via the fallback', () => {
    const edit = render({
      mode: 'edit',
      id: 'w1',
      record: { data: { id: 'w1' } },
    });
    expect(edit.result.current.record).toEqual({ id: 'w1' });

    const errored = render({
      mode: 'edit',
      id: 'w1',
      record: { error: { code: 2, message: '' } },
    });
    expect(errored.result.current.loadError).toBe('widget load failed');
  });
});

describe('useResourceForm — submit', () => {
  it('saves then navigates on success, clearing pending', async () => {
    saveSpy.mockClear();
    const { result, onDone, apiClient } = render({
      mode: 'edit',
      id: 'w1',
      record: { data: { id: 'w1' } },
    });
    act(() => result.current.mutate({ name: 'hi' }));
    await settle();
    expect(saveSpy).toHaveBeenCalledWith({
      apiClient,
      mode: 'edit',
      editing: { id: 'w1' },
      organization: 'acme',
      values: { name: 'hi' },
    });
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(result.current.pending).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it('surfaces an HTTP-error message and does NOT navigate', async () => {
    saveSpy.mockClear();
    saveSpy.mockResolvedValueOnce({ error: { code: 3, message: 'bad name' } });
    const { result, onDone } = render({ mode: 'create' });
    act(() => result.current.mutate({ name: '' }));
    await settle();
    expect(result.current.error).toBe('bad name');
    expect(result.current.pending).toBe(false);
    expect(onDone).not.toHaveBeenCalled();
  });

  it('catches a transport throw and surfaces the save fallback', async () => {
    saveSpy.mockClear();
    saveSpy.mockRejectedValueOnce(new Error('network down'));
    const { result, onDone } = render({ mode: 'create' });
    act(() => result.current.mutate({ name: 'x' }));
    await settle();
    expect(result.current.error).toBe('widget save failed');
    expect(result.current.pending).toBe(false);
    expect(onDone).not.toHaveBeenCalled();
  });
});

describe('useResourceForm — delete confirm', () => {
  it('opens the confirm, deletes, then navigates on success', async () => {
    removeSpy.mockClear();
    const { result, onDone, apiClient } = render({
      mode: 'edit',
      id: 'w1',
      record: { data: { id: 'w1' } },
    });
    act(() => result.current.onDelete?.());
    expect(result.current.remove.open).toBe(true);

    act(() => result.current.remove.onConfirm());
    await settle();
    expect(removeSpy).toHaveBeenCalledWith(apiClient, { id: 'w1' });
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it('surfaces a mapped delete error and keeps the confirm open', async () => {
    removeSpy.mockClear();
    removeSpy.mockResolvedValueOnce({
      error: { code: 9, message: 'still referenced' },
    });
    const { result, onDone } = render({
      mode: 'edit',
      id: 'w1',
      record: { data: { id: 'w1' } },
    });
    act(() => result.current.onDelete?.());
    act(() => result.current.remove.onConfirm());
    await settle();
    expect(result.current.remove.error).toBe('still referenced');
    expect(result.current.remove.pending).toBe(false);
    expect(onDone).not.toHaveBeenCalled();
  });

  it('does not dismiss the confirm mid-delete', () => {
    const { result } = render({
      mode: 'edit',
      id: 'w1',
      record: { data: { id: 'w1' } },
    });
    act(() => result.current.onDelete?.());
    // A pending delete blocks onOpenChange(false); with no delete in flight it closes.
    act(() => result.current.remove.onOpenChange(false));
    expect(result.current.remove.open).toBe(false);
  });
});
