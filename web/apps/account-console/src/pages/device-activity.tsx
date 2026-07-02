import { Button } from "@pivox/primitives/button";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { Monitor, Smartphone } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  deleteSession,
  getDevices,
  type DeviceRepresentation,
} from "@/api/account";
import { logout } from "@/keycloak";
import { label } from "@/user-profile/utils";

// KC stores session timestamps in seconds.
function formatWhen(ts?: number): string {
  return ts ? new Date(ts * 1000).toLocaleString() : "";
}

// Mirror KC: surface the current device (and its current session) at the top.
function moveCurrentToTop(
  devices: DeviceRepresentation[],
): DeviceRepresentation[] {
  const list = [...devices];
  const di = list.findIndex((d) => d.current);
  if (di > 0) {
    const [d] = list.splice(di, 1);
    list.unshift(d);
  }
  if (list.length === 0) return list;
  const cur = list[0];
  const sessions = cur.sessions ? [...cur.sessions] : undefined;
  if (sessions) {
    const si = sessions.findIndex((s) => s.current);
    if (si > 0) {
      const [s] = sessions.splice(si, 1);
      sessions.unshift(s);
    }
    list[0] = { ...cur, sessions };
  }
  return list;
}

export function DeviceActivity() {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<DeviceRepresentation[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getDevices()
      .then((d) => {
        setDevices(moveCurrentToTop(d));
      })
      .catch((e: unknown) => {
        setError(String(e));
      });
  }, []);

  useEffect(load, [load]);

  const signOutSession = async (id: string) => {
    if (!window.confirm(t("signOutWarning"))) return;
    try {
      await deleteSession(id);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  // Signing out ALL sessions kills the current one too, so KC logs you out
  // immediately after (a plain refresh would 401).
  const signOutAll = async () => {
    if (!window.confirm(t("signOutAllDevicesWarning"))) return;
    try {
      await deleteSession();
      logout();
    } catch (e) {
      setError(String(e));
    }
  };

  const clientLabel = (c: { clientId: string; clientName?: string }) =>
    label(t, c.clientName, c.clientId);

  const hasOthers =
    (devices?.length ?? 0) > 1 || (devices?.[0]?.sessions?.length ?? 0) > 1;

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">{t("deviceActivity")}</CardTitle>
        <CardDescription>{t("signedInDevicesExplanation")}</CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-4 px-4 pb-4">
        {error && <p className="text-sm text-destructive">{error}</p>}

        {devices?.map((device, i) => (
          <div
            key={device.id ?? i}
            className="flex flex-col gap-3 rounded-lg border border-border p-4"
          >
            <div className="flex items-center gap-3">
              <span className="text-muted-foreground">
                {device.mobile ? (
                  <Smartphone className="size-5" />
                ) : (
                  <Monitor className="size-5" />
                )}
              </span>
              <div className="flex flex-col">
                <span className="text-sm font-medium">
                  {[device.os, device.osVersion].filter(Boolean).join(" ") ||
                    device.device ||
                    t("unknownOperatingSystem")}
                  {device.browser ? ` · ${device.browser}` : ""}
                </span>
                <span className="text-xs text-muted-foreground">
                  {device.ipAddress}
                  {device.lastAccess
                    ? ` · ${t("lastAccess")} ${formatWhen(device.lastAccess)}`
                    : ""}
                </span>
              </div>
            </div>

            {device.sessions?.map((session) => (
              <div
                key={session.id}
                className="flex items-center justify-between gap-2 border-t border-border pt-3 text-sm"
              >
                <div className="flex flex-col">
                  <span>
                    {(session.clients ?? []).map(clientLabel).join(", ")}
                    {session.current && (
                      <span className="ms-2 rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
                        {t("currentSession")}
                      </span>
                    )}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {t("started")} {formatWhen(session.started)}
                  </span>
                </div>
                {!session.current && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void signOutSession(session.id)}
                  >
                    {t("doSignOut")}
                  </Button>
                )}
              </div>
            ))}
          </div>
        ))}

        {devices && devices.length === 0 && (
          <p className="text-sm text-muted-foreground">{t("signedInDevices")}</p>
        )}

        {hasOthers && (
          <div className="pb-1">
            <Button variant="destructive" onClick={() => void signOutAll()}>
              {t("signOutAllDevices")}
            </Button>
          </div>
        )}
      </div>
    </Card>
  );
}
