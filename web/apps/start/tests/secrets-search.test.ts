import { DEFAULT_PAGE_SIZE } from '@pivox/ui/resource-admin';
import { describe, expect, it } from 'vitest';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

import {
  searchToValue,
  validateSecretsSearch,
  valueToSearch,
} from '../src/lib/secrets-search';

const clean: ListControlsValue = {
  filters: {},
  sort: null,
  pageSize: DEFAULT_PAGE_SIZE,
  scope: '',
  pageToken: undefined,
};

describe('secrets search params', () => {
  it('omits defaults so a clean list has a bare URL', () => {
    expect(valueToSearch(clean)).toEqual({});
  });

  it('round-trips filter / scope / size / cursor', () => {
    const value: ListControlsValue = {
      filters: { displayName: 'stripe' },
      sort: { field: 'createTime', direction: 'desc' },
      pageSize: 50,
      scope: 'main',
      pageToken: 'tok',
    };
    const search = valueToSearch(value);
    expect(search).toEqual({
      q: 'stripe',
      scope: 'main',
      sort: 'createTime',
      dir: 'desc',
      size: 50,
      cursor: 'tok',
    });
    expect(searchToValue(search)).toEqual(value);
  });

  it('drops the ascending default from the URL', () => {
    expect(
      valueToSearch({
        ...clean,
        sort: { field: 'displayName', direction: 'asc' },
      }),
    ).toEqual({ sort: 'displayName' });
  });

  it('validateSearch coerces valid params and rejects junk', () => {
    expect(
      validateSecretsSearch({ q: 'x', size: '50', dir: 'sideways', junk: 1 }),
    ).toEqual({ q: 'x', size: 50 });
    // 7 isn't an offered page size.
    expect(validateSecretsSearch({ size: '7' })).toEqual({});
  });
});
