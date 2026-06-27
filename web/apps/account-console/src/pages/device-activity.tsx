import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { Button } from "@pivox/primitives/button";
import { Monitor, Smartphone } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import {
  deleteSession,
  getDevices,
  resolveLabel,
  type DeviceRepresentation,
} from "@/api/account";

function formatWhen(ts?: number): string {
  if (!ts) return "";
  return new Date(ts * 1000).toLocaleString();
}

const clientLabel = (client: { clientId: string; clientName?: string }) =>
  resolveLabel(client.clientName, client.clientId);

export function DeviceActivity() {
  const [devices, setDevices] = useState<DeviceRepresentation[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getDevices()
      .then(setDevices)
      .catch((e: unknown) => setError(String(e)));
  }, []);

  useEffect(load, [load]);

  const signOut = async (id?: string) => {
    try {
      await deleteSession(id);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  const hasOthers = devices?.some((d) =>
    d.sessions?.some((s) => !s.current),
  );

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">Device activity</CardTitle>
        <CardDescription>
          Sign out of devices that don&apos;t look familiar
        </CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-4 px-4">
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
                    "Unknown device"}
                  {device.browser ? ` · ${device.browser}` : ""}
                </span>
                <span className="text-xs text-muted-foreground">
                  {device.ipAddress}
                  {device.lastAccess
                    ? ` · last active ${formatWhen(device.lastAccess)}`
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
                    {(session.clients ?? []).map(clientLabel).join(", ") ||
                      "Session"}
                    {session.current && (
                      <span className="ms-2 rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
                        Current
                      </span>
                    )}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    Started {formatWhen(session.started)}
                  </span>
                </div>
                {!session.current && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void signOut(session.id)}
                  >
                    Sign out
                  </Button>
                )}
              </div>
            ))}
          </div>
        ))}

        {devices && devices.length === 0 && (
          <p className="text-sm text-muted-foreground">No active sessions.</p>
        )}

        {hasOthers && (
          <div className="pb-1">
            <Button
              variant="destructive"
              onClick={() => void signOut(undefined)}
            >
              Sign out of all other devices
            </Button>
          </div>
        )}
      </div>
    </Card>
  );
}
