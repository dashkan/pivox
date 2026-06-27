import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { Field, FieldError, FieldLabel } from "@pivox/primitives/field";
import { Input } from "@pivox/primitives/input";
import { Button } from "@pivox/primitives/button";
import { useEffect, useState } from "react";

import {
  getPersonalInfo,
  savePersonalInfo,
  type UserRepresentation,
} from "@/api/account";

export function PersonalInfo() {
  const [user, setUser] = useState<UserRepresentation | null>(null);
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">(
    "idle",
  );
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getPersonalInfo()
      .then(setUser)
      .catch((e: unknown) => setError(String(e)));
  }, []);

  const update = (patch: Partial<UserRepresentation>) =>
    setUser((u) => (u ? { ...u, ...patch } : u));

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!user) return;
    setStatus("saving");
    setError(null);
    try {
      await savePersonalInfo({
        email: user.email,
        firstName: user.firstName,
        lastName: user.lastName,
      });
      setStatus("saved");
    } catch (err) {
      setStatus("error");
      setError(String(err));
    }
  };

  return (
    <Card className="w-full max-w-xl">
      <CardHeader>
        <CardTitle className="text-xl">Personal info</CardTitle>
        <CardDescription>Manage your basic account information</CardDescription>
      </CardHeader>

      {user === null && !error ? (
        <p className="px-4 text-sm text-muted-foreground">Loading…</p>
      ) : (
        <form onSubmit={onSubmit} className="flex flex-col gap-4">
          <Field className="px-4">
            <FieldLabel>Username</FieldLabel>
            <Input value={user?.username ?? ""} disabled readOnly />
          </Field>

          <Field className="px-4">
            <FieldLabel>Email</FieldLabel>
            <Input
              type="email"
              autoComplete="email"
              value={user?.email ?? ""}
              onChange={(e) => update({ email: e.target.value })}
            />
          </Field>

          <Field className="px-4">
            <FieldLabel>First name</FieldLabel>
            <Input
              value={user?.firstName ?? ""}
              onChange={(e) => update({ firstName: e.target.value })}
            />
          </Field>

          <Field className="px-4">
            <FieldLabel>Last name</FieldLabel>
            <Input
              value={user?.lastName ?? ""}
              onChange={(e) => update({ lastName: e.target.value })}
            />
          </Field>

          <div className="flex flex-col gap-3 px-4">
            {error && <FieldError>{error}</FieldError>}
            {status === "saved" && (
              <p className="text-sm text-emerald-600 dark:text-emerald-400">
                Your changes have been saved.
              </p>
            )}
            <div className="flex items-center gap-2">
              <Button type="submit" disabled={status === "saving"}>
                {status === "saving" ? "Saving…" : "Save"}
              </Button>
            </div>
          </div>
        </form>
      )}
    </Card>
  );
}
