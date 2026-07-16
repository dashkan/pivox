import { describe, expect, it } from 'vitest';

import {
  AGENT_FILTER_ANY,
  AGENT_FILTER_CLOUD,
} from '@pivox/ui/resource-admin';

import { buildConnectorFilter } from '@/connectors/build-connector-filter';

describe('buildConnectorFilter', () => {
  it('returns undefined when no filters are active', () => {
    expect(buildConnectorFilter({})).toBeUndefined();
    expect(buildConnectorFilter({ displayName: '  ' })).toBeUndefined();
    expect(
      buildConnectorFilter({ agent: AGENT_FILTER_ANY }),
    ).toBeUndefined();
  });

  it('builds a substring predicate for the display name', () => {
    expect(buildConnectorFilter({ displayName: 'stripe' })).toBe(
      'displayName:"stripe"',
    );
  });

  it('emits an empty-agent equality for the Cloud sentinel', () => {
    expect(buildConnectorFilter({ agent: AGENT_FILTER_CLOUD })).toBe(
      'agent=""',
    );
  });

  it('emits an exact-agent equality for a resource name', () => {
    const agent = 'organizations/acme/storageGateways/gw/agents/a1';
    expect(buildConnectorFilter({ agent })).toBe(`agent="${agent}"`);
  });

  it('ANDs an active name and agent filter', () => {
    expect(
      buildConnectorFilter({
        displayName: 'stripe',
        agent: AGENT_FILTER_CLOUD,
      }),
    ).toBe('displayName:"stripe" AND agent=""');
  });

  it('escapes a quote in the display name value', () => {
    expect(buildConnectorFilter({ displayName: 'a"b' })).toBe(
      'displayName:"a\\"b"',
    );
  });
});
