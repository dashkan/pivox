import { Button } from "@pivox/primitives/button";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { ExternalLink } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { deleteConsent, getApplications, type Application } from "@/api/account";
import { label } from "@/user-profile/utils";

export function Applications() {
  const { t } = useTranslation();
  const [apps, setApps] = useState<Application[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getApplications()
      .then(setApps)
      .catch((e: unknown) => {
        setError(String(e));
      });
  }, []);

  useEffect(load, [load]);

  // Revokes consent AND offline tokens for the client (KC keys by clientId).
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
        <CardTitle className="text-xl">{t("applications")}</CardTitle>
        <CardDescription>{t("applicationsIntroMessage")}</CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-3 px-4 pb-4">
        {error && <p className="text-sm text-destructive">{error}</p>}

        {apps?.map((app) => {
          const name = label(t, app.clientName, app.clientId);
          return (
            <div
              key={app.clientId}
              className="flex items-center justify-between gap-3 rounded-lg border border-border p-4"
            >
              <div className="flex flex-col gap-1">
                {app.effectiveUrl ? (
                  <a
                    href={app.effectiveUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline"
                  >
                    {name} <ExternalLink className="size-3" />
                  </a>
                ) : (
                  <span className="text-sm font-medium">{name}</span>
                )}
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  {app.inUse && (
                    <span className="rounded bg-primary/10 px-1.5 py-0.5 font-medium text-primary">
                      {t("inUse")}
                    </span>
                  )}
                  {app.offlineAccess && <span>{t("offlineAccess")}</span>}
                </div>
              </div>
              {(app.consent ?? app.offlineAccess) && (
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => void revoke(app.clientId)}
                >
                  {t("removeAccess")}
                </Button>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}
