import { createFileRoute } from '@tanstack/react-router';

// Placeholder so OrgGateFeature's redirect has a target. The real
// create-org form (mirroring native Auth/CreateOrgView.swift) lands in
// Phase 7 task 3.
export const Route = createFileRoute('/auth/create-org')({
  component: CreateOrgPlaceholder,
});

function CreateOrgPlaceholder() {
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-2 text-center">
        <h1 className="text-lg font-medium">Create your organization</h1>
        <p className="text-sm text-muted-foreground">
          The create-org form is coming next.
        </p>
      </div>
    </div>
  );
}
