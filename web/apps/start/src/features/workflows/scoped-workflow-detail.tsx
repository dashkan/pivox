import { organizationId, parseResourceName } from '@pivox/client';
import { useWorkflowDefinition } from '@pivox/features/workflows';
import { Badge } from '@pivox/primitives/badge';
import { Button } from '@pivox/primitives/button';
import { Field, FieldError, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import { Switch } from '@pivox/primitives/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@pivox/primitives/tabs';
import { Textarea } from '@pivox/primitives/textarea';
import { actorLabel, AdminNotice, formatTimestamp } from '@pivox/ui/resource-admin';
import { WorkflowCanvas } from '@pivox/ui/workflow';
import { useRouter } from '@tanstack/react-router';

import { useState } from 'react';

import type { ApiClient, components } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';

import { $api, apiClient } from '@/lib/api-client';
import { resolveReturnTo } from '@/lib/return-to';

type Workflow = components['schemas']['v1Workflow'];
type WorkflowVersion = components['schemas']['v1WorkflowVersion'];

const WORKFLOW_PATH = '/v1/organizations/{organization}/workflows/{workflow}' as const;
const SPACE_WORKFLOW_PATH =
  '/v1/organizations/{organization}/spaces/{space}/workflows/{workflow}' as const;
const VERSIONS_PATH =
  '/v1/organizations/{organization}/workflows/{workflow}/versions' as const;
const SPACE_VERSIONS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/workflows/{workflow}/versions' as const;

/** The scoped workflows LIST path for this route's `?from=` fallback. */
function scopedWorkflowsListRoute(orgSlug: string, spaceSlug?: string): string {
  return spaceSlug
    ? `/organizations/${orgSlug}/spaces/${spaceSlug}/workflows`
    : `/organizations/${orgSlug}/workflows`;
}

function versionId(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).versions ?? '';
}

/**
 * Scoped workflow CANVAS DETAIL shell for the URL-scoped route tree. The org (and,
 * on the space route, the space) come from the route's path params, and the
 * "← Workflows" back link soft-navigates to the scoped workflows list — honoring
 * `?from=` (sanitized) so a filtered/sorted list view is preserved on return, else
 * the scope's list. `spaceSlug` threads into the record/versions/settings queries
 * so a space-scoped workflow resolves on its own REST path.
 */
export function ScopedWorkflowDetail({
  orgSlug,
  spaceSlug,
  workflowId,
  from,
}: {
  orgSlug: string;
  /** Present on the space-scoped route; absent on the org route. */
  spaceSlug?: string;
  workflowId: string;
  from?: string;
}) {
  const router = useRouter();
  const returnTo = resolveReturnTo(
    from,
    scopedWorkflowsListRoute(orgSlug, spaceSlug),
  );

  return (
    <WorkflowDetail
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      space={spaceSlug}
      workflowId={workflowId}
      back={
        <a
          href={returnTo}
          className="text-sm text-muted-foreground hover:underline"
          onClick={(e) => {
            e.preventDefault();
            router.history.push(returnTo);
          }}
        >
          ← Workflows
        </a>
      }
    />
  );
}

/**
 * The tabbed workflow detail body (definition canvas / versions / settings /
 * runs). Router-free and `$api`/`apiClient`-injected so it unit-tests without a
 * router; `ScopedWorkflowDetail` wraps it with the scoped back link.
 */
export function WorkflowDetail({
  $api: api,
  apiClient: client,
  parent,
  space,
  workflowId,
  back,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  /** Space slug for a space-scoped workflow; absent = org-direct. */
  space?: string;
  workflowId: string;
  back: React.ReactNode;
}) {
  const organization = organizationId(parent);
  // Both queries are declared (stable hook count); only the one matching the
  // scope is enabled — the same pattern the list descriptor uses.
  const orgQuery = api.useQuery(
    'get',
    WORKFLOW_PATH,
    { params: { path: { organization, workflow: workflowId } } },
    { enabled: !space },
  );
  const spaceQuery = api.useQuery(
    'get',
    SPACE_WORKFLOW_PATH,
    { params: { path: { organization, space: space ?? '', workflow: workflowId } } },
    { enabled: !!space },
  );
  const workflowQuery = space ? spaceQuery : orgQuery;
  const workflow = workflowQuery.data;

  return (
    <div className="flex flex-1 flex-col gap-6 p-6">
      <div className="flex flex-col gap-2">
        {back}
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
              space={space}
              workflowId={workflowId}
              liveVersion={workflow.version}
            />
          </TabsContent>

          <TabsContent value="settings">
            <SettingsForm
              apiClient={client}
              organization={organization}
              space={space}
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
  space,
  workflowId,
  liveVersion,
}: {
  $api: ReactQueryApi;
  organization: string;
  space?: string;
  workflowId: string;
  liveVersion: string | undefined;
}) {
  // Both queries declared (stable hook count); only the scoped one is enabled.
  const orgVersionsQuery = api.useQuery(
    'get',
    VERSIONS_PATH,
    { params: { path: { organization, workflow: workflowId } } },
    { enabled: !space },
  );
  const spaceVersionsQuery = api.useQuery(
    'get',
    SPACE_VERSIONS_PATH,
    { params: { path: { organization, space: space ?? '', workflow: workflowId } } },
    { enabled: !!space },
  );
  const versionsQuery = space ? spaceVersionsQuery : orgVersionsQuery;
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
  space,
  workflowId,
  workflow,
  onSaved,
}: {
  apiClient: ApiClient;
  organization: string;
  space?: string;
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
      const body = {
        displayName,
        description,
        enabled,
        ...(workflow.etag ? { etag: workflow.etag } : {}),
      };
      const resp = space
        ? await client.PATCH(SPACE_WORKFLOW_PATH, {
            params: { path: { organization, space, workflow: workflowId } },
            body,
          })
        : await client.PATCH(WORKFLOW_PATH, {
            params: { path: { organization, workflow: workflowId } },
            body,
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
