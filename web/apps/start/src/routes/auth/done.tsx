import { createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/auth/done')({
  component: DonePage,
});

function DonePage() {
  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">
        You can close this browser tab and return to the Pivox desktop app.
      </p>
    </div>
  );
}
