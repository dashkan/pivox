import { Button } from "@pivox/primitives/button";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import type { TFunction } from "i18next";

import {
  getLinkedAccounts,
  unLinkAccount,
  type LinkedAccount,
} from "@/api/account";
import { keycloak } from "@/keycloak";
import { label } from "@/user-profile/utils";

function AccountRow({
  account,
  t,
  action,
}: {
  account: LinkedAccount;
  t: TFunction;
  action: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-border p-4">
      <div className="flex flex-col">
        <span className="text-sm font-medium">
          {label(t, account.displayName, account.providerName)}
        </span>
        <span className="text-xs text-muted-foreground">
          {t(account.social ? "socialLogin" : "systemDefined")}
          {account.linkedUsername ? ` · ${account.linkedUsername}` : ""}
        </span>
      </div>
      {action}
    </div>
  );
}

export function LinkedAccounts() {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<LinkedAccount[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getLinkedAccounts()
      .then(setAccounts)
      .catch((e: unknown) => {
        setError(String(e));
      });
  }, []);

  useEffect(load, [load]);

  // KC's account-link flow is an Application-Initiated Action, not a REST
  // redirect — `idp_link:<alias>` re-authenticates, runs KC's link/confirm
  // screen (including the "already linked to another user" guard), then returns
  // here. The old accountLinkUri REST path is deprecated and skips all of that.
  const link = (account: LinkedAccount) => {
    void keycloak.login({ action: `idp_link:${account.providerAlias}` });
  };

  const unlink = async (account: LinkedAccount) => {
    try {
      // KC unlinks by providerName, not the alias used for linking.
      await unLinkAccount(account.providerName);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  const linked = (accounts ?? []).filter((a) => a.connected);
  const unlinked = (accounts ?? []).filter((a) => !a.connected);

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">{t("linkedAccountsSidebarTitle")}</CardTitle>
        <CardDescription>{t("linkedAccountsIntroMessage")}</CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-6 px-4 pb-4">
        {error && <p className="text-sm text-destructive">{error}</p>}

        <section className="flex flex-col gap-3">
          <h2 className="text-sm font-semibold">{t("linkedLoginProviders")}</h2>
          {linked.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("linkedEmpty")}</p>
          ) : (
            linked.map((a) => (
              <AccountRow
                key={a.providerAlias}
                account={a}
                t={t}
                action={
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void unlink(a)}
                  >
                    {t("unLink")}
                  </Button>
                }
              />
            ))
          )}
        </section>

        <section className="flex flex-col gap-3">
          <h2 className="text-sm font-semibold">{t("unlinkedLoginProviders")}</h2>
          {unlinked.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("unlinkedEmpty")}</p>
          ) : (
            unlinked.map((a) => (
              <AccountRow
                key={a.providerAlias}
                account={a}
                t={t}
                action={
                  <Button
                    size="sm"
                    onClick={() => {
                      link(a);
                    }}
                  >
                    {t("doLink")}
                  </Button>
                }
              />
            ))
          )}
        </section>
      </div>
    </Card>
  );
}
