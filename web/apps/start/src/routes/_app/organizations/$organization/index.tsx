import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';
import { createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/_app/organizations/$organization/')({
  component: OrgHomePage,
});

/**
 * Org home — the landing target after root/selector resolve a scope. A minimal
 * app-themed overview stub for now; it grows into a dashboard later. The active
 * org is already resolved + validated by the `$organization` layout.
 */
function OrgHomePage() {
  const { organization } = Route.useParams();
  return (
    <div className="flex flex-1 flex-col gap-4 p-6">
      <div>
        <h1 className="text-2xl font-semibold">Overview</h1>
        <p className="text-sm text-muted-foreground">
          {organization}
        </p>
      </div>
      <Card className="max-w-md">
        <CardHeader>
          <CardTitle>Welcome</CardTitle>
          <CardDescription>
            This is your organization home. Use the sidebar to manage connectors
            and other resources.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          A richer dashboard lands here in a later iteration.
        </CardContent>
      </Card>
    </div>
  );
}
