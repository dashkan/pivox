import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { useEffect, useState } from "react";

import { getGroups, type Group } from "@/api/account";

export function Groups() {
  const [groups, setGroups] = useState<Group[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getGroups()
      .then(setGroups)
      .catch((e: unknown) => setError(String(e)));
  }, []);

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">Groups</CardTitle>
        <CardDescription>Groups you are a member of</CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-2 px-4">
        {error ? (
          <p className="text-sm text-muted-foreground">
            Groups aren&apos;t available for your account.
          </p>
        ) : (
          groups?.map((g, i) => (
            <div
              key={g.id ?? g.path ?? i}
              className="rounded-lg border border-border px-4 py-3 text-sm"
            >
              <span className="font-medium">{g.name ?? g.path}</span>
              {g.path && g.path !== g.name && (
                <span className="ms-2 text-xs text-muted-foreground">
                  {g.path}
                </span>
              )}
            </div>
          ))
        )}
        {!error && groups && groups.length === 0 && (
          <p className="text-sm text-muted-foreground">
            You are not a member of any groups.
          </p>
        )}
      </div>
    </Card>
  );
}
