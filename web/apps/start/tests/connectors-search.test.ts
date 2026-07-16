import {
  AGENT_FILTER_ANY,
  AGENT_FILTER_CLOUD,
  DEFAULT_PAGE_SIZE,
} from '@pivox/ui/resource-admin';
import { describe, expect, it } from 'vitest';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

import {
  searchToValue,
  validateConnectorsSearch,
  valueToSearch,
} from '../src/lib/connectors-search';

const clean: ListControlsValue = {
  filters: {},
  sort: null,
  pageSize: DEFAULT_PAGE_SIZE,
  scope: '',
  pageToken: undefined,
};

describe('connectors search params', () => {
  it('omits defaults so a clean list has a bare URL', () => {
    expect(valueToSearch(clean)).toEqual({});
  });

  it('round-trips filter / sort / scope / size / cursor', () => {
    const value: ListControlsValue = {
      filters: { displayName: 'stripe', agent: AGENT_FILTER_CLOUD },
      sort: { field: 'updateTime', direction: 'desc' },
      pageSize: 50,
      scope: 'main',
      pageToken: 'tok',
    };
    const search = valueToSearch(value);
    expect(search).toEqual({
      q: 'stripe',
      agent: AGENT_FILTER_CLOUD,
      scope: 'main',
      sort: 'updateTime',
      dir: 'desc',
      size: 50,
      cursor: 'tok',
    });
    expect(searchToValue(search)).toEqual(value);
  });

  it('drops the ANY agent sentinel and ascending default from the URL', () => {
    expect(
      valueToSearch({
        ...clean,
        filters: { agent: AGENT_FILTER_ANY },
        sort: { field: 'displayName', direction: 'asc' },
      }),
    ).toEqual({ sort: 'displayName' });
  });

  it('validateSearch coerces valid params and rejects junk', () => {
    expect(
      validateConnectorsSearch({
        q: 'x',
        size: '50',
        dir: 'sideways',
        junk: 1,
      }),
    ).toEqual({ q: 'x', size: 50 });
    // 7 isn't an offered page size.
    expect(validateConnectorsSearch({ size: '7' })).toEqual({});
  });
});
