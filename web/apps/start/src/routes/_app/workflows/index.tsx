import { organizationId, parseResourceName } from '@pivox/client';
import { Badge } from '@pivox/primitives/badge';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@pivox/primitives/table';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { actorLabel, AdminNotice, formatTimestamp } from '@pivox/ui/resource-admin';
import { Link, createFileRoute } from '@tanstack/react-router';

import type { components } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';

import { $api } from '@/lib/api-client';

type Workflow = components['schemas']['v1Workflow'];

const WORKFLOWS_PATH = '/v1/organizations/{organization}/workflows' as const;

export const Route = createFileRoute('/_app/workflows/')({
  component: WorkflowsRoute,
});

function WorkflowsRoute() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to view workflows.</AdminNotice>
      </div>
    );
  }

  return <WorkflowsList $api={$api} parent={parent} />;
}

/** Origin badge: MANAGED workflows are Pivox-owned, OWNED are the customer's. */
function OriginBadge({ origin }: { origin?: Workflow['origin'] }) {
  return origin === 'MANAGED' ? (
    <Badge variant="secondary">Managed</Badge>
  ) : (
    <Badge variant="outline">Owned</Badge>
  );
}

function versionLabel(version: string | undefined): string {
  if (!version) return '—';
  return parseResourceName(version).versions ?? '—';
}

export function WorkflowsList({
  $api: api,
  parent,
}: {
  $api: ReactQueryApi;
  parent: string;
}) {
  const listQuery = api.useQuery('get', WORKFLOWS_PATH, {
    params: { path: { organization: organizationId(parent) } },
  });

  const workflows = listQuery.data?.workflows ?? [];

  return (
    <div className="flex flex-1 flex-col gap-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Workflows</h1>
        <p className="text-sm text-muted-foreground">
          Authored and Pivox-managed workflow definitions.
        </p>
      </div>
      {listQuery.isLoading ? (
        <AdminNotice>Loading workflows…</AdminNotice>
      ) : listQuery.error ? (
        <AdminNotice>Couldn&apos;t load workflows.</AdminNotice>
      ) : workflows.length === 0 ? (
        <AdminNotice>No workflows yet.</AdminNotice>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Origin</TableHead>
              <TableHead>Enabled</TableHead>
              <TableHead>Live version</TableHead>
              <TableHead>Updated</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {workflows.map((workflow) => {
              const workflowId = parseResourceName(workflow.name ?? '').workflows ?? '';
              return (
                <TableRow key={workflow.name}>
                  <TableCell className="font-medium">
                    <Link
                      to="/workflows/$workflowId"
                      params={{ workflowId }}
                      className="hover:underline"
                    >
                      {workflow.displayName || workflowId}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <OriginBadge origin={workflow.origin} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {workflow.enabled ? 'Enabled' : 'Disabled'}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {versionLabel(workflow.version)}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {formatTimestamp(workflow.updateTime)} · {actorLabel(workflow.updatedBy)}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
