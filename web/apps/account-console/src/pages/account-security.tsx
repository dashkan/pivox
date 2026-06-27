import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { Button } from "@pivox/primitives/button";
import { useCallback, useEffect, useState } from "react";

import {
  deleteCredential,
  getCredentials,
  type CredentialContainer,
} from "@/api/account";
import { runAction } from "@/keycloak";

const TYPE_LABELS: Record<string, string> = {
  password: "Password",
  otp: "Authenticator application",
  webauthn: "Security key",
  "webauthn-passwordless": "Passkey",
  "recovery-authn-codes": "Recovery codes",
};

const CATEGORY_LABELS: Record<string, string> = {
  "basic-authentication": "Basic authentication",
  "two-factor": "Two-factor authentication",
  passwordless: "Passwordless",
};

const typeLabel = (type: string) => TYPE_LABELS[type] ?? type;
const fmtDate = (ts?: number) => (ts ? new Date(ts).toLocaleDateString() : "");

export function AccountSecurity() {
  const [containers, setContainers] = useState<CredentialContainer[] | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    getCredentials()
      .then(setContainers)
      .catch((e: unknown) => setError(String(e)));
  }, []);

  useEffect(load, [load]);

  const remove = async (id: string) => {
    try {
      await deleteCredential(id);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">Account security</CardTitle>
        <CardDescription>
          Manage your password and two-factor authentication
        </CardDescription>
      </CardHeader>

      <div className="flex flex-col gap-4 px-4">
        {error && <p className="text-sm text-destructive">{error}</p>}

        {containers?.map((c) => (
          <div
            key={c.type}
            className="flex flex-col gap-3 rounded-lg border border-border p-4"
          >
            <div className="flex items-center justify-between gap-3">
              <div className="flex flex-col">
                <span className="text-sm font-medium">{typeLabel(c.type)}</span>
                <span className="text-xs text-muted-foreground">
                  {CATEGORY_LABELS[c.category] ?? c.category}
                </span>
              </div>
              <div className="flex items-center gap-2">
                {c.updateAction && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => runAction(c.updateAction!)}
                  >
                    Update
                  </Button>
                )}
                {c.createAction && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => runAction(c.createAction!)}
                  >
                    Set up
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
                      {m.credential.userLabel || typeLabel(c.type)}
                      {m.credential.createdDate ? (
                        <span className="text-xs text-muted-foreground">
                          {" "}
                          · created {fmtDate(m.credential.createdDate)}
                        </span>
                      ) : null}
                    </span>
                    {c.removeable && (
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => void remove(m.credential.id)}
                      >
                        Remove
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="border-t border-border pt-3 text-xs text-muted-foreground">
                Not set up.
              </p>
            )}
          </div>
        ))}

        {containers && containers.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No credentials configured.
          </p>
        )}
      </div>
    </Card>
  );
}
