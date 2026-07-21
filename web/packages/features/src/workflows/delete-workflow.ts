import { parseResourceName } from '@pivox/client';

import type { ApiClient } from '@pivox/client';
import type { Workflow } from '@pivox/ui/resource-admin';

const WORKFLOW_PATH =
  '/v1/organizations/{organization}/workflows/{workflow}' as const;
const SPACE_WORKFLOW_PATH =
  '/v1/organizations/{organization}/spaces/{space}/workflows/{workflow}' as const;

/**
 * Deletes a workflow by its full name, threading the optimistic-concurrency etag
 * — the list-tier `remove` the `ListDescriptor` requires. The workflows list does
 * not wire a row-delete action (workflows are managed on the canvas), so this is
 * not triggered from a column today; it is the correct, forward-compatible
 * implementation of the required hook rather than a stub. Handles the space-scoped
 * path too, though the org-direct list only ever yields org-direct rows.
 */
export function deleteWorkflow(input: {
  apiClient: ApiClient;
  workflow: Workflow;
}) {
  const { apiClient, workflow } = input;
  const parts = parseResourceName(workflow.name ?? '');
  const organization = parts.organizations ?? '';
  const workflowId = parts.workflows ?? '';
  const space = parts.spaces;
  const etagQuery = workflow.etag ? { etag: workflow.etag } : {};
  return space
    ? apiClient.DELETE(SPACE_WORKFLOW_PATH, {
        params: {
          path: { organization, space, workflow: workflowId },
          query: etagQuery,
        },
      })
    : apiClient.DELETE(WORKFLOW_PATH, {
        params: {
          path: { organization, workflow: workflowId },
          query: etagQuery,
        },
      });
}
