import { describe, expect, it } from 'vitest';

import {
  buildConnectorBody,
  entriesToMap,
} from '@/connectors/build-connector-body';

import type { ConnectorFormValues } from '@pivox/ui/resource-admin';

const base: ConnectorFormValues = {
  connectorId: 'stripe',
  displayName: 'Stripe',
  description: 'Payments',
  baseUrl: 'https://api.stripe.com',
  headers: [],
  agent: '',
  scope: '',
};

describe('entriesToMap', () => {
  it('builds a map and drops blank-key rows', () => {
    expect(
      entriesToMap([
        { key: 'Authorization', value: 'Bearer x' },
        { key: '  ', value: 'ignored' },
        { key: '', value: 'ignored' },
      ]),
    ).toEqual({ Authorization: 'Bearer x' });
  });

  it('trims keys but preserves values verbatim', () => {
    expect(entriesToMap([{ key: '  X-Api-Key ', value: ' secret("…") ' }])).toEqual(
      { 'X-Api-Key': ' secret("…") ' },
    );
  });
});

describe('buildConnectorBody', () => {
  it('nests base_url under http and trims it', () => {
    const body = buildConnectorBody({ ...base, baseUrl: '  https://api.x  ' });
    expect(body.http?.baseUrl).toBe('https://api.x');
  });

  it('always sets http.headers, even empty, so an N→0 clear de-scopes server-side', () => {
    // Body-derived PATCH mask: an omitted `headers` would keep the old value,
    // so clearing the last row must send `{}` to actually remove the headers.
    const cleared = buildConnectorBody({ ...base, headers: [] });
    expect(cleared.http?.headers).toEqual({});

    const onlyBlank = buildConnectorBody({
      ...base,
      headers: [{ key: '', value: 'v' }],
    });
    expect(onlyBlank.http?.headers).toEqual({});
  });

  it('includes headers when present', () => {
    const body = buildConnectorBody({
      ...base,
      headers: [{ key: 'Authorization', value: 'secret("…/secrets/x")' }],
    });
    expect(body.http?.headers).toEqual({
      Authorization: 'secret("…/secrets/x")',
    });
  });

  it('trims agent and never sets a resource name', () => {
    const body = buildConnectorBody({ ...base, agent: '  organizations/acme/agents/a1 ' });
    expect(body.agent).toBe('organizations/acme/agents/a1');
    expect(body.name).toBeUndefined();
  });
});
