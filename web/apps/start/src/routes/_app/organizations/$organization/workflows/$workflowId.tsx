import { createFileRoute } from '@tanstack/react-router';

import { ScopedWorkflowDetail } from '@/features/workflows/scoped-workflow-detail';

/** Search for the detail route: the launching list route to return to (sanitized). */
function validateDetailSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/workflows/$workflowId',
)({
  validateSearch: validateDetailSearch,
  component: WorkflowDetailPage,
});

function WorkflowDetailPage() {
  const { organization, workflowId } = Route.useParams();
  const { from } = Route.useSearch();
  return (
    <ScopedWorkflowDetail
      orgSlug={organization}
      workflowId={workflowId}
      from={from}
    />
  );
}
