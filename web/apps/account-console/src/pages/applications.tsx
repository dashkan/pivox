import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { Button } from "@pivox/primitives/button";
import { ExternalLink } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import {
  deleteConsent,
  getApplications,
  resolveLabel,
  type Application,
} from "@/api/account";

export function Applications() {
  const [apps, setApps] = useState<Application[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getApplications()
      .then(setApps)
      .catch((e: unknown) => setError(String(e)));
  }, []);

  useEffect(load, [load]);

  const revoke = async (clientId: string) => {
    try {
      await deleteConsent(clientId);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">Applications</CardTitle>
        <CardDescription>
          Applications that have access to your account
        </CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-3 px-4">
        {error && <p className="text-sm text-destructive">{error}</p>}

        {apps?.map((app) => (
          <div
            key={app.clientId}
            className="flex items-center justify-between gap-3 rounded-lg border border-border p-4"
          >
            <div className="flex flex-col gap-1">
              <span className="text-sm font-medium">
                {resolveLabel(app.clientName, app.clientId)}
              </span>
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                {app.inUse && (
                  <span className="rounded bg-primary/10 px-1.5 py-0.5 font-medium text-primary">
                    In use
                  </span>
                )}
                {app.offlineAccess && <span>Offline access</span>}
                {app.effectiveUrl && (
                  <a
                    href={app.effectiveUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline"
                  >
                    Open <ExternalLink className="size-3" />
                  </a>
                )}
              </div>
            </div>
            {app.consent && (
              <Button
                variant="destructive"
                size="sm"
                onClick={() => void revoke(app.clientId)}
              >
                Remove access
              </Button>
            )}
          </div>
        ))}

        {apps && apps.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No applications have access to your account.
          </p>
        )}
      </div>
    </Card>
  );
}
