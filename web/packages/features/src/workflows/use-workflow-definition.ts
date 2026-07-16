'use client';

import { parseResourceName } from '@pivox/client';
import { useEffect, useMemo, useState } from 'react';

import type { ReactQueryApi } from '@pivox/client/react-query';

import { describeRpcError } from '@/resource-admin/rpc-error';
import { astToGraph } from '@/workflows/transform/ast-to-graph';
import type { WorkflowGraph } from '@/workflows/transform/ast-to-graph';
import { layoutGraph } from '@/workflows/transform/layout';
import { isSpaceScoped, resourcePathParams } from '@/workflows/resource-paths';

// Org-scoped routes, matching the Phase 1 surfaces (connectors/secrets are the
// same). Space-scoped workflow names are not wired here yet.
const WORKFLOW_ORG = '/v1/organizations/{organization}/workflows/{workflow}' as const;
const VERSION_ORG =
  '/v1/organizations/{organization}/workflows/{workflow}/versions/{version}' as const;

/** Domain state for the definition canvas — never a raw react-query result. */
export type WorkflowDefinitionState = {
  /** Laid-out graph for the resolved version, or null until it is ready. */
  graph: WorkflowGraph | null;
  isLoading: boolean;
  error: string | null;
};

/** True when `name` addresses a WorkflowVersion rather than its container Workflow. */
function isVersionName(name: string): boolean {
  return parseResourceName(name).versions !== undefined;
}

/**
 * Loads a workflow definition and lays it out for the canvas.
 *
 * `name` is either a WorkflowVersion resource name (loaded directly) or a
 * Workflow resource name — in which case the live `version` is resolved via
 * GetWorkflow, then that version is fetched. The version's `root`/`error_sequence`
 * run through `astToGraph` (memoized on version identity) and then `layoutGraph`
 * (async elk); the laid-out result is committed via an effect that ignores a
 * resolution arriving after the version changed or the hook unmounted.
 */
export function useWorkflowDefinition(input: {
  $api: ReactQueryApi;
  name: string;
}): WorkflowDefinitionState {
  const { $api, name } = input;

  // Org-only for now: the query paths above omit the {space} segment, so a
  // space-scoped name would misfire against `/v1/organizations/.../workflows/...`
  // and 404 silently. Fail loudly until the space path variants are wired.
  if (isSpaceScoped(name)) {
    throw new Error(
      `useWorkflowDefinition: space-scoped names are not supported yet (got "${name}")`,
    );
  }

  const asVersion = useMemo(() => isVersionName(name), [name]);

  // GetWorkflow — only fired when `name` is a Workflow, to resolve its live version.
  const workflowParams = useMemo(() => resourcePathParams(name), [name]);
  const workflowQuery = $api.useQuery(
    'get',
    WORKFLOW_ORG,
    {
      params: {
        path: {
          organization: workflowParams.organization ?? '',
          workflow: workflowParams.workflow ?? '',
        },
      },
    },
    { enabled: !asVersion },
  );

  // The version to render: `name` itself, or the live version of the Workflow.
  // An unpromoted Workflow yields an empty `version`, leaving the target unset.
  const targetVersionName = asVersion ? name : workflowQuery.data?.version;
  const versionParams = useMemo(
    () => (targetVersionName ? resourcePathParams(targetVersionName) : {}),
    [targetVersionName],
  );
  const versionQuery = $api.useQuery(
    'get',
    VERSION_ORG,
    {
      params: {
        path: {
          organization: versionParams.organization ?? '',
          workflow: versionParams.workflow ?? '',
          version: versionParams.version ?? '',
        },
      },
    },
    { enabled: !!targetVersionName },
  );

  const version = versionQuery.data;
  const astGraph = useMemo(() => (version ? astToGraph(version) : null), [version]);

  // Layout output tagged with the astGraph it was computed from, so a graph is
  // only surfaced when it matches the current version's astGraph — a stale
  // resolution (version switched mid-layout) never leaks through.
  const [laid, setLaid] = useState<{
    source: WorkflowGraph;
    graph: WorkflowGraph;
  } | null>(null);
  const [layoutError, setLayoutError] = useState<string | null>(null);

  useEffect(() => {
    if (!astGraph) return undefined;
    let active = true;
    setLayoutError(null);
    layoutGraph(astGraph).then(
      (graph) => {
        if (active) setLaid({ source: astGraph, graph });
      },
      (err: unknown) => {
        if (active) {
          setLayoutError(err instanceof Error ? err.message : String(err));
        }
      },
    );
    return () => {
      active = false;
    };
  }, [astGraph]);

  const graph = laid && laid.source === astGraph ? laid.graph : null;

  const queryLoading =
    (!asVersion && workflowQuery.isLoading) ||
    (!!targetVersionName && versionQuery.isLoading);
  const layingOut = astGraph !== null && graph === null && layoutError === null;

  const error =
    (workflowQuery.error
      ? describeRpcError(workflowQuery.error, "Couldn't load the workflow.")
      : null) ??
    (versionQuery.error
      ? describeRpcError(versionQuery.error, "Couldn't load the definition.")
      : null) ??
    layoutError;

  return {
    graph,
    isLoading: queryLoading || layingOut,
    error,
  };
}
