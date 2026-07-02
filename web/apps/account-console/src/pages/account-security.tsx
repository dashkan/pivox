import { Button } from "@pivox/primitives/button";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { getCredentials, type CredentialContainer } from "@/api/account";
import { runAction } from "@/keycloak";
import { label } from "@/user-profile/utils";

function fmtDate(ts?: number): string {
  return ts ? new Date(ts).toLocaleDateString() : "";
}

/**
 * "Signing in" — mirrors KC account-ui's SigningIn page. Credentials are grouped
 * by category; set-up / update / remove are all Application-Initiated Actions
 * (`keycloak.login({action})`), NOT REST — `container.createAction`/`updateAction`
 * are server-supplied, and removal is the `delete_credential:<id>` kcAction (KC
 * shows its own confirm). Labels come from the server (`displayName`/`helptext`),
 * resolved through the message bundle.
 */
export function AccountSecurity() {
  const { t } = useTranslation();
  const [containers, setContainers] = useState<CredentialContainer[] | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getCredentials()
      .then(setContainers)
      .catch((e: unknown) => {
        setError(String(e));
      });
  }, []);

  useEffect(load, [load]);

  const categories = [...new Set((containers ?? []).map((c) => c.category))];

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">{t("signingIn")}</CardTitle>
        <CardDescription>{t("signingInDescription")}</CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-8 px-4 pb-4">
        {error && <p className="text-sm text-destructive">{error}</p>}

        {categories.map((category) => (
          <section key={category} className="flex flex-col gap-3">
            <h2 className="text-sm font-semibold">{label(t, category)}</h2>
            {(containers ?? [])
              .filter((c) => c.category === category)
              .map((c) => {
                const name = label(t, c.displayName, c.type);
                const { createAction, updateAction } = c;
                return (
                  <div
                    key={c.type}
                    className="flex flex-col gap-3 rounded-lg border border-border p-4"
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div className="flex flex-col">
                        <span className="text-sm font-medium">{name}</span>
                        {c.helptext && (
                          <span className="text-xs text-muted-foreground">
                            {label(t, c.helptext, "")}
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-2">
                        {updateAction && (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              runAction(updateAction);
                            }}
                          >
                            {t("update")}
                          </Button>
                        )}
                        {createAction && (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              runAction(createAction);
                            }}
                          >
                            {t("setUpNew", { name })}
                          </Button>
                        )}
                      </div>
                    </div>

                    {c.userCredentialMetadatas.length > 0 ? (
                      <div className="flex flex-col gap-2 border-t border-border pt-3">
                        {c.userCredentialMetadatas.map((m) => (
                          <div
                            key={m.credential.id}
                            className="flex items-center justify-between gap-2 text-sm"
                          >
                            <span>
                              {m.credential.userLabel || name}
                              {m.credential.createdDate ? (
                                <span className="text-xs text-muted-foreground">
                                  {" · "}
                                  {fmtDate(m.credential.createdDate)}
                                </span>
                              ) : null}
                            </span>
                            {c.removeable && (
                              <Button
                                variant="destructive"
                                size="sm"
                                onClick={() => {
                                  runAction(
                                    `delete_credential:${m.credential.id}`,
                                  );
                                }}
                              >
                                {t("doRemove")}
                              </Button>
                            )}
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="border-t border-border pt-3 text-xs text-muted-foreground">
                        {t("notSetUp", { name })}
                      </p>
                    )}
                  </div>
                );
              })}
          </section>
        ))}
      </div>
    </Card>
  );
}
