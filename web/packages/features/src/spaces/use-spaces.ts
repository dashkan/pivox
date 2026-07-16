'use client';

import { useMemo } from 'react';

import type { ReactQueryApi } from '@pivox/client/react-query';
import type { SpaceOption } from '@pivox/ui/resource-admin';

import { toSpaceOptions } from '@/spaces/space-options';
import { resourcePathParams } from '@/workflows/resource-paths';

const SPACES_PATH = '/v1/organizations/{organization}/spaces' as const;

/**
 * Lists the spaces under an org as domain `SpaceOption`s (no react-query type
 * leaks). `parent` is the org resource name (`organizations/{slug}`).
 */
export function useSpaces(input: {
  $api: ReactQueryApi;
  parent: string;
}): { spaces: SpaceOption[]; isLoading: boolean } {
  const { $api, parent } = input;
  const organization = useMemo(
    () => resourcePathParams(parent).organization ?? '',
    [parent],
  );

  const query = $api.useQuery('get', SPACES_PATH, {
    params: { path: { organization } },
  });

  const spaces = useMemo<SpaceOption[]>(
    () => toSpaceOptions(query.data?.spaces ?? []),
    [query.data],
  );

  return { spaces, isLoading: query.isLoading };
}
