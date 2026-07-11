import { Button } from "@pivox/primitives/button";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card";
import { FieldError } from "@pivox/primitives/field";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { getPersonalInfo, RequestError, savePersonalInfo } from "@/api/account";
import { keycloak } from "@/keycloak";
import { UserProfileFields } from "@/user-profile/UserProfileFields";
import {
  applyServerErrors,
  beerify,
  debeerify,
  type ProfileMetadata,
  type UserProfileFormValues,
} from "@/user-profile/utils";

export function PersonalInfo() {
  const { t } = useTranslation();
  const [metadata, setMetadata] = useState<ProfileMetadata | null>(null);
  const [values, setValues] = useState<UserProfileFormValues | undefined>(
    undefined,
  );
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">(
    "idle",
  );
  const [error, setError] = useState<string | null>(null);

  // `values` re-syncs the form when the async data arrives — the RHF pattern for
  // data loaded after mount. (reset()/setValue() before the fields register
  // silently drops the values, which is why the form rendered empty.)
  const form = useForm<UserProfileFormValues>({ values });

  useEffect(() => {
    getPersonalInfo()
      .then((info) => {
        // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- KC account REST `/?userProfileMetadata=true` response carries this blob, typed `unknown` at the API boundary (UserRepresentation.userProfileMetadata).
        const meta = info.userProfileMetadata as ProfileMetadata | undefined;
        setMetadata(meta ?? null);
        // KC returns every attribute as a string[]; unwrap single-valued ones so
        // the plain inputs bind, keep arrays for multivalued.
        const multivalued = new Set(
          (meta?.attributes ?? [])
            .filter((a) => a.multivalued)
            .map((a) => a.name),
        );
        const attributes: Record<string, string | string[]> = {};
        for (const [k, vals] of Object.entries(info.attributes ?? {})) {
          attributes[beerify(k)] = multivalued.has(k) ? vals : (vals[0] ?? "");
        }
        setValues({
          username: info.username,
          email: info.email,
          firstName: info.firstName,
          lastName: info.lastName,
          attributes,
        });
      })
      .catch((e: unknown) => {
        setError(String(e));
      });
  }, []);

  const onSubmit = form.handleSubmit(async (data) => {
    setStatus("saving");
    setError(null);
    form.clearErrors();
    try {
      // Re-wrap into KC's string[] shape, un-escaping dotted attribute names.
      const raw = data.attributes ?? {};
      const attributes: Record<string, string[]> = {};
      // RHF reports disabled (read-only) fields as `undefined` — a case the form
      // value type doesn't model, so widen it here to skip them below.
      const entries = Object.entries(raw) as [
        string,
        string | string[] | undefined,
      ][];
      for (const [k, v] of entries) {
        // Read-only fields report `undefined` from RHF — skip them so we never
        // send `[null]` (KC ignores read-only on update anyway).
        if (v == null) continue;
        const fieldValues = (Array.isArray(v) ? v : [v]).filter(
          (s): s is string => typeof s === "string" && s !== "",
        );
        // Send even when empty so clearing an optional field persists, matching
        // KC's own console (which posts the full form).
        attributes[debeerify(k)] = fieldValues;
      }
      await savePersonalInfo({ ...data, attributes });
      await keycloak.updateToken(30);
      setStatus("saved");
    } catch (err) {
      setStatus("error");
      // KC returns field-level validation errors as JSON on 400 — surface them
      // under each field (like account-ui) instead of a bare status.
      const errorData = err instanceof RequestError ? err.responseData : undefined;
      if (errorData && typeof errorData === "object") {
        applyServerErrors(
          errorData,
          (field, fieldError) => {
            form.setError(field, fieldError);
          },
          t,
        );
      } else {
        setError(String(err));
      }
    }
  });

  return (
    <Card className="w-full max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">{t("personalInfoSidebarTitle")}</CardTitle>
        <CardDescription>{t("personalInfoIntroMessage")}</CardDescription>
      </CardHeader>

      {!metadata && !error ? (
        <p className="px-4 text-sm text-muted-foreground">Loading…</p>
      ) : (
        <form
          onSubmit={(e) => {
            void onSubmit(e);
          }}
          className="flex flex-col gap-6 px-4 pb-4"
        >
          {metadata && <UserProfileFields form={form} metadata={metadata} t={t} />}
          {error && <FieldError>{error}</FieldError>}
          {status === "saved" && (
            <p className="text-sm text-emerald-600 dark:text-emerald-400">
              {t("accountUpdatedMessage")}
            </p>
          )}
          <div className="flex items-center gap-2">
            <Button type="submit" disabled={status === "saving"}>
              {status === "saving" ? "…" : t("doSave")}
            </Button>
            {/* Reverts the form to the loaded values — KC's account-ui uses a
                bare reset() (back to the last-loaded profile) behind t("cancel"). */}
            <Button
              type="button"
              variant="link"
              disabled={status === "saving"}
              onClick={() => {
                form.reset();
                setStatus("idle");
                setError(null);
              }}
            >
              {t("cancel")}
            </Button>
          </div>
        </form>
      )}
    </Card>
  );
}
