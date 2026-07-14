import { Badge } from '@pivox/primitives/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@pivox/primitives/card';
import { createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/_app/')({ component: HomePage });

function HomePage() {
  return (
    <div className="flex flex-1 items-center justify-center p-8">
      <Card className="w-full max-w-md">
        <CardHeader>
          <div className="flex items-center gap-2">
            <CardTitle className="text-2xl">Pivox</CardTitle>
            <Badge variant="secondary">Electron</Badge>
          </div>
          <CardDescription>Desktop Application</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            Shared UI components working across Next.js, Electron, and TanStack
            Start.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
