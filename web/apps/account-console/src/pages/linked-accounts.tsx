import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { Button } from "@pivox/primitives/button";
import { useCallback, useEffect, useState } from "react";

import {
  getLinkedAccounts,
  linkAccount,
  unLinkAccount,
  type LinkedAccount,
} from "@/api/account";

export function LinkedAccounts() {
  const [accounts, setAccounts] = useState<LinkedAccount[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getLinkedAccounts()
      .then(setAccounts)
      .catch((e: unknown) => setError(String(e)));
  }, []);

  useEffect(load, [load]);

  const link = async (alias: string) => {
    try {
      const { accountLinkUri } = await linkAccount(alias);
      window.location.href = accountLinkUri;
    } catch (e) {
      setError(String(e));
    }
  };

  const unlink = async (alias: string) => {
    try {
      await unLinkAccount(alias);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">Linked accounts</CardTitle>
        <CardDescription>
          Connect identity providers to sign in faster
        </CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-3 px-4">
        {error && <p className="text-sm text-destructive">{error}</p>}

        {accounts?.map((acct) => (
          <div
            key={acct.providerAlias}
            className="flex items-center justify-between gap-3 rounded-lg border border-border p-4"
          >
            <div className="flex flex-col">
              <span className="text-sm font-medium">
                {acct.displayName || acct.providerName}
              </span>
              <span className="text-xs text-muted-foreground">
                {acct.connected
                  ? `Connected${acct.linkedUsername ? ` as ${acct.linkedUsername}` : ""}`
                  : "Not connected"}
              </span>
            </div>
            {acct.connected ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => void unlink(acct.providerAlias)}
              >
                Unlink
              </Button>
            ) : (
              <Button size="sm" onClick={() => void link(acct.providerAlias)}>
                Link
              </Button>
            )}
          </div>
        ))}

        {accounts && accounts.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No identity providers are configured.
          </p>
        )}
      </div>
    </Card>
  );
}
