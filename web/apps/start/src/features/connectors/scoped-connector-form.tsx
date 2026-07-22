import {
  ConnectorCreateFeature,
  ConnectorEditFeature,
  fetchAgentOptions,
} from '@pivox/features/connectors';
import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';

import { $api, apiClient } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import { useConnectorFormNav } from '@/lib/use-connector-form-nav';

// Agent membership changes rarely; keep the SSR-primed options fresh so no
// gateways/agents XHR fires on page load.
const AGENTS_STALE_TIME = 5 * 60 * 1000;

/** The scoped connectors LIST path for this route's `?from=` fallback. */
function scopedListRoute(orgSlug: string, spaceSlug?: string): string {
  return spaceSlug
    ? `/organizations/${orgSlug}/spaces/${spaceSlug}/connectors`
    : `/organizations/${orgSlug}/connectors`;
}

function useScopedAgentOptions(orgSlug: string) {
  const agentsQuery = useQuery({
    queryKey: connectorAgentsQueryKey(orgSlug),
    queryFn: () => fetchAgentOptions(apiClient, orgSlug),
    staleTime: AGENTS_STALE_TIME,
  });
  return useMemo(() => agentsQuery.data ?? [], [agentsQuery.data]);
}

/** The shared "← Connectors" back link — a soft navigation to the return target. */
function BackLink({ returnTo, goBack }: { returnTo: string; goBack: () => void }) {
  return (
    <a
      href={returnTo}
      className="hover:underline"
      onClick={(e) => {
        e.preventDefault();
        goBack();
      }}
    >
      ← Connectors
    </a>
  );
}

/**
 * Shared connector CREATE page for the scope-in-URL routes. Both the org-rollup
 * `/connectors/new` and the space `.../spaces/$space/connectors/new` render this;
 * `spaceSlug` only pins the return target (the create form owns scope selection).
 */
export function ScopedConnectorNew({
  orgSlug,
  spaceSlug,
  from,
}: {
  orgSlug: string;
  spaceSlug?: string;
  from?: string;
}) {
  const { returnTo, goBack, goBackAndRefresh, onDirtyChange } =
    useConnectorFormNav(from, scopedListRoute(orgSlug, spaceSlug));
  const agentOptions = useScopedAgentOptions(orgSlug);

  return (
    <ConnectorCreateFeature
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      agentOptions={agentOptions}
      back={<BackLink returnTo={returnTo} goBack={goBack} />}
      onCancel={goBack}
      onSubmitSuccess={goBackAndRefresh}
      onDirtyChange={onDirtyChange}
    />
  );
}

/**
 * Shared connector EDIT page for the scope-in-URL routes. The connector's scope
 * is the route's scope: the org-direct route passes no space; the space route
 * passes its `$space` param straight through to the edit feature.
 */
export function ScopedConnectorEdit({
  orgSlug,
  spaceSlug,
  connectorId,
  from,
}: {
  orgSlug: string;
  spaceSlug?: string;
  connectorId: string;
  from?: string;
}) {
  const { returnTo, goBack, goBackAndRefresh, onDirtyChange } =
    useConnectorFormNav(from, scopedListRoute(orgSlug, spaceSlug));
  const agentOptions = useScopedAgentOptions(orgSlug);

  return (
    <ConnectorEditFeature
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      connectorId={connectorId}
      space={spaceSlug}
      agentOptions={agentOptions}
      back={<BackLink returnTo={returnTo} goBack={goBack} />}
      onCancel={goBack}
      onSubmitSuccess={goBackAndRefresh}
      onDirtyChange={onDirtyChange}
    />
  );
}
