import { describe, expect, it } from 'vitest';

import { toAgentOptions } from '@/connectors/agent-options';

import type { components } from '@pivox/client/types';

type Agent = components['schemas']['v1Agent'];

describe('toAgentOptions', () => {
  it('maps value to the resource name and label to the hostname', () => {
    const agents: Agent[] = [
      {
        name: 'organizations/acme/storageGateways/gw1/agents/a1',
        hostname: 'edge-01',
      },
    ];
    expect(toAgentOptions(agents)).toEqual([
      {
        value: 'organizations/acme/storageGateways/gw1/agents/a1',
        label: 'edge-01',
      },
    ]);
  });

  it('falls back to the id leaf when the hostname is unreported', () => {
    const agents: Agent[] = [
      { name: 'organizations/acme/storageGateways/gw1/agents/a1' },
    ];
    expect(toAgentOptions(agents)).toEqual([
      {
        value: 'organizations/acme/storageGateways/gw1/agents/a1',
        label: 'a1',
      },
    ]);
  });

  it('drops agents without a resource name (no selectable identity)', () => {
    const agents: Agent[] = [{ hostname: 'orphan' }];
    expect(toAgentOptions(agents)).toEqual([]);
  });

  it('flattens agents gathered across multiple gateways', () => {
    const agents: Agent[] = [
      { name: 'organizations/acme/storageGateways/gw1/agents/a1', hostname: 'edge-01' },
      { name: 'organizations/acme/storageGateways/gw2/agents/a2', hostname: 'edge-02' },
    ];
    expect(toAgentOptions(agents).map((o) => o.label)).toEqual([
      'edge-01',
      'edge-02',
    ]);
  });
});
