import {
  Card,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { Checkbox } from "@pivox/primitives/checkbox";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { getGroups, type Group } from "@/api/account";

/**
 * Mirror KC account-ui's Groups page: `/groups` returns only DIRECT memberships
 * (each with an `id`). When "direct membership" is off (the default), KC also
 * shows the indirect parent groups, synthesized client-side by walking each
 * group's `path` upward — synthesized ancestors have no `id`, which is exactly
 * how the "direct membership" column is derived (`g.id != null`).
 */
function withAncestors(groups: Group[]): Group[] {
  const byPath = new Map(
    groups.filter((g) => g.path).map((g) => [g.path, g] as const),
  );
  const result = [...groups];
  for (const g of groups) {
    if (!g.path) continue;
    const segments = g.path.split("/").filter(Boolean);
    for (let i = 1; i < segments.length; i++) {
      const path = `/${segments.slice(0, i).join("/")}`;
      if (!byPath.has(path)) {
        const parent: Group = { name: segments[i - 1], path };
        byPath.set(path, parent);
        result.push(parent);
      }
    }
  }
  return result.sort((a, b) => (a.path ?? "").localeCompare(b.path ?? ""));
}

export function Groups() {
  const { t } = useTranslation();
  const [groups, setGroups] = useState<Group[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [directOnly, setDirectOnly] = useState(false);

  useEffect(() => {
    getGroups()
      .then(setGroups)
      .catch((e: unknown) => {
        setError(String(e));
      });
  }, []);

  const displayed = directOnly
    ? (groups ?? []).filter((g) => g.id != null)
    : withAncestors(groups ?? []);

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">{t("groups")}</CardTitle>
      </CardHeader>

      <div className="flex flex-col gap-3 px-4 pb-4">
        {error ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : (
          <>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={directOnly}
                onCheckedChange={(c) => {
                  setDirectOnly(c);
                }}
              />
              {t("directMembership")}
            </label>

            {displayed.map((g, i) => (
              <div
                key={g.id ?? g.path ?? i}
                className="flex items-center justify-between gap-2 rounded-lg border border-border px-4 py-3 text-sm"
              >
                <div className="flex flex-col">
                  <span className="font-medium">{g.name ?? g.path}</span>
                  {g.path && g.path !== `/${g.name ?? ""}` && (
                    <span className="text-xs text-muted-foreground">
                      {g.path}
                    </span>
                  )}
                </div>
                <Checkbox
                  checked={g.id != null}
                  disabled
                  aria-label={t("directMembership")}
                />
              </div>
            ))}

            {groups && displayed.length === 0 && (
              <p className="text-sm text-muted-foreground">{t("noGroupsText")}</p>
            )}
          </>
        )}
      </div>
    </Card>
  );
}
