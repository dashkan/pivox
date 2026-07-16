import { organizationId, parseResourceName } from '@pivox/client';
import { useWorkflowDefinition } from '@pivox/features/workflows';
import { Badge } from '@pivox/primitives/badge';
import { Button } from '@pivox/primitives/button';
import { Field, FieldError, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import { Switch } from '@pivox/primitives/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@pivox/primitives/tabs';
import { Textarea } from '@pivox/primitives/textarea';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { actorLabel, AdminNotice, formatTimestamp } from '@pivox/ui/resource-admin';
import { WorkflowCanvas } from '@pivox/ui/workflow';
import { Link, createFileRoute } from '@tanstack/react-router';
import { useState } from 'react';

import type { ApiClient, components } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';

import { $api, apiClient } from '@/lib/api-client';

type Workflow = components['schemas']['v1Workflow'];
type WorkflowVersion = components['schemas']['v1WorkflowVersion'];

const WORKFLOW_PATH = '/v1/organizations/{organization}/workflows/{workflow}' as const;
const VERSIONS_PATH =
  '/v1/organizations/{organization}/workflows/{workflow}/versions' as const;

export const Route = createFileRoute('/_app/workflows/$workflowId')({
  component: WorkflowDetailRoute,
});

function WorkflowDetailRoute() {
  const { workflowId } = Route.useParams();
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to view this workflow.</AdminNotice>
      </div>
    );
  }

  return (
    <WorkflowDetail
      $api={$api}
      apiClient={apiClient}
      parent={parent}
      workflowId={workflowId}
    />
  );
}

function versionId(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).versions ?? '';
}

export function WorkflowDetail({
  $api: api,
  apiClient: client,
  parent,
  workflowId,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  workflowId: string;
}) {
  const organization = organizationId(parent);
  const workflowQuery = api.useQuery('get', WORKFLOW_PATH, {
    params: { path: { organization, workflow: workflowId } },
  });
  const workflow = workflowQuery.data;

  return (
    <div className="flex flex-1 flex-col gap-6 p-6">
      <div className="flex flex-col gap-2">
        <Link to="/workflows" className="text-sm text-muted-foreground hover:underline">
          ← Workflows
        </Link>
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">
            {workflow?.displayName || workflowId}
          </h1>
          {workflow && (
            <Badge variant={workflow.origin === 'MANAGED' ? 'secondary' : 'outline'}>
              {workflow.origin === 'MANAGED' ? 'Managed' : 'Owned'}
            </Badge>
          )}
          {workflow && (
            <Badge variant={workflow.enabled ? 'default' : 'ghost'}>
              {workflow.enabled ? 'Enabled' : 'Disabled'}
            </Badge>
          )}
        </div>
      </div>

      {workflowQuery.isLoading ? (
        <AdminNotice>Loading workflow…</AdminNotice>
      ) : workflowQuery.error || !workflow ? (
        <AdminNotice>Couldn&apos;t load this workflow.</AdminNotice>
      ) : (
        <Tabs defaultValue="definition" className="flex-1">
          <TabsList>
            <TabsTrigger value="definition">Definition</TabsTrigger>
            <TabsTrigger value="versions">Versions</TabsTrigger>
            <TabsTrigger value="settings">Settings</TabsTrigger>
            <TabsTrigger value="runs">Runs</TabsTrigger>
          </TabsList>

          <TabsContent value="definition">
            {workflow.version ? (
              <DefinitionCanvas $api={api} versionName={workflow.version} />
            ) : (
              <AdminNotice>
                This workflow has no promoted version yet.
              </AdminNotice>
            )}
          </TabsContent>

          <TabsContent value="versions">
            <VersionsTab
              $api={api}
              organization={organization}
              workflowId={workflowId}
              liveVersion={workflow.version}
            />
          </TabsContent>

          <TabsContent value="settings">
            <SettingsForm
              apiClient={client}
              organization={organization}
              workflowId={workflowId}
              workflow={workflow}
              onSaved={() => {
                void workflowQuery.refetch();
              }}
            />
          </TabsContent>

          <TabsContent value="runs">
            <AdminNotice>Runs are coming in Phase 2.</AdminNotice>
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}

/** Loads a version and renders its laid-out definition on the read-only canvas. */
function DefinitionCanvas({
  $api: api,
  versionName,
}: {
  $api: ReactQueryApi;
  versionName: string;
}) {
  const { graph, isLoading, error } = useWorkflowDefinition({
    $api: api,
    name: versionName,
  });

  if (isLoading) return <AdminNotice>Laying out the definition…</AdminNotice>;
  if (error) return <AdminNotice>{error}</AdminNotice>;
  if (!graph) return <AdminNotice>No definition to display.</AdminNotice>;

  return (
    <div className="h-[70vh] w-full overflow-hidden rounded-lg border">
      <WorkflowCanvas nodes={graph.nodes} edges={graph.edges} />
    </div>
  );
}

function VersionsTab({
  $api: api,
  organization,
  workflowId,
  liveVersion,
}: {
  $api: ReactQueryApi;
  organization: string;
  workflowId: string;
  liveVersion: string | undefined;
}) {
  const versionsQuery = api.useQuery('get', VERSIONS_PATH, {
    params: { path: { organization, workflow: workflowId } },
  });
  const versions = versionsQuery.data?.workflowVersions ?? [];

  const [selected, setSelected] = useState<string | undefined>(liveVersion);
  const active = selected ?? liveVersion ?? versions[0]?.name;

  if (versionsQuery.isLoading) return <AdminNotice>Loading versions…</AdminNotice>;
  if (versionsQuery.error) return <AdminNotice>Couldn&apos;t load versions.</AdminNotice>;
  if (versions.length === 0) return <AdminNotice>No versions yet.</AdminNotice>;

  return (
    <div className="flex gap-4">
      <ul className="flex w-64 shrink-0 flex-col gap-1">
        {versions.map((version: WorkflowVersion) => {
          const isLive = !!version.name && version.name === liveVersion;
          const isActive = version.name === active;
          return (
            <li key={version.name}>
              <button
                type="button"
                onClick={() => setSelected(version.name)}
                className={`flex w-full flex-col items-start gap-1 rounded-md border p-3 text-left text-sm transition-colors hover:bg-muted ${
                  isActive ? 'border-primary bg-muted' : 'border-transparent'
                }`}
              >
                <span className="flex items-center gap-2 font-medium">
                  v{versionId(version.name)}
                  {isLive && <Badge variant="default">Live</Badge>}
                </span>
                {version.note && (
                  <span className="text-muted-foreground">{version.note}</span>
                )}
                <span className="text-xs text-muted-foreground">
                  {formatTimestamp(version.createTime)} · {actorLabel(version.createdBy)}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
      <div className="flex-1">
        {active ? (
          <DefinitionCanvas $api={api} versionName={active} />
        ) : (
          <AdminNotice>Select a version.</AdminNotice>
        )}
      </div>
    </div>
  );
}

/**
 * Minimal container-edit form: display name, description, enabled. Mutates via
 * PATCH UpdateWorkflow; grpc-gateway derives the field mask from body presence.
 */
function SettingsForm({
  apiClient: client,
  organization,
  workflowId,
  workflow,
  onSaved,
}: {
  apiClient: ApiClient;
  organization: string;
  workflowId: string;
  workflow: Workflow;
  onSaved: () => void;
}) {
  const [displayName, setDisplayName] = useState(workflow.displayName ?? '');
  const [description, setDescription] = useState(workflow.description ?? '');
  const [enabled, setEnabled] = useState(workflow.enabled ?? false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = () => {
    setPending(true);
    setError(null);
    void (async () => {
      const resp = await client.PATCH(WORKFLOW_PATH, {
        params: { path: { organization, workflow: workflowId } },
        body: {
          displayName,
          description,
          enabled,
          ...(workflow.etag ? { etag: workflow.etag } : {}),
        },
      });
      setPending(false);
      if (resp.error) {
        setError(resp.error.message ?? "Couldn't save changes.");
        return;
      }
      onSaved();
    })();
  };

  return (
    <form
      className="flex max-w-xl flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          disabled={pending}
        />
      </Field>
      <Field>
        <FieldLabel>Description</FieldLabel>
        <Textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          disabled={pending}
        />
      </Field>
      <Field orientation="horizontal">
        <Switch
          checked={enabled}
          onCheckedChange={setEnabled}
          disabled={pending}
        />
        <FieldLabel>Enabled</FieldLabel>
      </Field>
      {error && <FieldError>{error}</FieldError>}
      <div>
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : 'Save changes'}
        </Button>
      </div>
    </form>
  );
}
