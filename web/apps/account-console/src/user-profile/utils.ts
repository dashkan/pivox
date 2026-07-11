import type {
  UserProfileAttributeMetadata,
  UserProfileMetadata,
} from "@/user-profile/keycloak-types";
import type { TFunction } from "i18next";
import type { Path } from "react-hook-form";

/**
 * User Profile rendering helpers, ported from Keycloak's ui-shared
 * `user-profile/utils.ts` so the Pivox-primitives renderer matches upstream
 * behaviour exactly (label resolution, field-path escaping, input-type
 * selection). Kept as a faithful port rather than importing from account-ui
 * because these live in an internal (non-exported) module there.
 */

/** The four attributes Keycloak stores on the user root, not under `attributes`. */
const ROOT_ATTRIBUTES = ["username", "firstName", "lastName", "email"];

export function isRootAttribute(attr?: string): boolean {
  return !!attr && ROOT_ATTRIBUTES.includes(attr);
}

/** True when a displayName/text is a message-bundle key, e.g. `${profile.x}`. */
export function isBundleKey(text?: string): boolean {
  return typeof text === "string" ? text.includes("${") : false;
}

function unWrap(key: string): string {
  return key.substring(2, key.length - 1);
}

/**
 * Resolves a label through the KC message bundle: `${key}` → `t(key)`, plain
 * text → `t(text)` (which falls back to the text itself when unknown).
 */
export function label(
  t: TFunction,
  text?: string,
  fallback?: string,
  prefix?: string,
): string {
  const value = text || fallback;
  const bundleKey = value && isBundleKey(value) ? unWrap(value) : value;
  const key = prefix ? `${prefix}.${bundleKey}` : bundleKey;
  return t(key ?? "");
}

export function labelAttribute(
  t: TFunction,
  attribute: UserProfileAttributeMetadata,
): string {
  return label(t, attribute.displayName, attribute.name);
}

export function isRequiredAttribute(
  attribute: UserProfileAttributeMetadata,
): boolean {
  return attribute.required;
}

/**
 * react-hook-form treats `.` as a nested-path separator, but KC attribute names
 * can contain dots. Upstream escapes them with a beer emoji on the form path and
 * un-escapes on submit — we mirror that so dotted attribute names round-trip.
 */
export function beerify(name: string): string {
  return name.replaceAll(".", "🍺");
}

export function debeerify(name: string): string {
  return name.replaceAll("🍺", ".");
}

/** The react-hook-form field path for an attribute. */
export function fieldName(name?: string): Path<UserProfileFormValues> {
  const path = `${isRootAttribute(name) ? "" : "attributes."}${beerify(name ?? "")}`;
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- react-hook-form's Path<> is a compile-time-only template-literal brand; this string is a valid UserProfileFormValues path (root name or `attributes.<beerified>`) but TS can't prove it from the template.
  return path as Path<UserProfileFormValues>;
}

export type ServerFieldError = {
  field: string;
  errorMessage: string;
  params?: unknown[];
};

/**
 * Ported from account-ui's `setUserProfileServerError`: maps KC's field-level
 * validation errors (the `errors` array in a 400 body, or a single error) onto
 * react-hook-form field errors. Each `errorMessage`/`param` that is a
 * `${bundle.key}` is unwrapped + translated; the `params` array becomes
 * positional interpolation ({{0}}, {{1}}, …) so e.g. `error-invalid-uri-scheme`
 * ("'{{0}}' has invalid URL scheme.") resolves against the field label.
 */
export function applyServerErrors(
  responseData: unknown,
  setError: (
    field: Path<UserProfileFormValues>,
    error: { message: string; type: string },
  ) => void,
  t: TFunction,
): void {
  // The KC 400 body is either `{ errors: ServerFieldError[] }` or a single
  // ServerFieldError; the intersection lets us read `.errors` when present and
  // otherwise wrap the body itself, without a second assertion.
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- KC account REST 400 validation body; shape is documented on RequestError and there is no runtime schema validator for it.
  const body = responseData as { errors?: ServerFieldError[] } & ServerFieldError;
  const errors = body.errors ?? [body];
  for (const e of errors) {
    // KC's `params` array becomes positional interpolation values keyed by
    // index (0, 1, …); ${bundle.key} params are unwrapped + translated.
    const params: Record<string, unknown> = {};
    (e.params ?? []).forEach((p, i) => {
      params[i] = typeof p === "string" && isBundleKey(p) ? t(unWrap(p)) : p;
    });
    const key = isBundleKey(e.errorMessage)
      ? unWrap(e.errorMessage)
      : e.errorMessage;
    setError(fieldName(e.field), {
      message: t(key, { ...params, defaultValue: e.errorMessage || e.field }),
      type: "server",
    });
  }
}

/**
 * inputType values that map to an `<input>` (the html5-* set) — the rest have
 * dedicated controls. Mirrors upstream's FIELDS keys.
 */
const INPUT_TYPES = [
  "text",
  "textarea",
  "select",
  "select-radiobuttons",
  "multiselect",
  "multiselect-checkboxes",
  "html5-email",
  "html5-tel",
  "html5-url",
  "html5-number",
  "html5-range",
  "html5-datetime-local",
  "html5-date",
  "html5-month",
  "html5-time",
] as const;

export type InputType = (typeof INPUT_TYPES)[number];

function isInputType(value: unknown): value is InputType {
  return (
    typeof value === "string" &&
    (INPUT_TYPES as readonly string[]).includes(value)
  );
}

/**
 * Resolves the effective input type: root attributes are always plain text;
 * otherwise honour `annotations.inputType` when it's a known type; default text.
 */
export function determineInputType(
  attribute: UserProfileAttributeMetadata,
): InputType {
  if (isRootAttribute(attribute.name)) return "text";
  const annotated: unknown = attribute.annotations?.inputType;
  return isInputType(annotated) ? annotated : "text";
}

/**
 * account-ui's published types omit the `group` (per attribute) and `groups`
 * (per metadata) fields that KC's REST response actually carries — declare the
 * fuller shapes we render against.
 */
export type UserProfileGroupMetadata = {
  name?: string;
  displayHeader?: string;
  displayDescription?: string;
  annotations?: Record<string, unknown>;
};
export type AttributeMetadata = UserProfileAttributeMetadata & { group?: string };
export type ProfileMetadata = UserProfileMetadata & {
  groups?: UserProfileGroupMetadata[];
};

/** Ordered groups to render: the ungrouped bucket first, then named groups. */
export function orderedGroups(
  metadata: ProfileMetadata,
): UserProfileGroupMetadata[] {
  return [{ name: undefined }, ...(metadata.groups ?? [])];
}

export function attributesForGroup(
  metadata: ProfileMetadata,
  groupName?: string,
): AttributeMetadata[] {
  return (
    (metadata.attributes as AttributeMetadata[] | undefined) ?? []
  ).filter((a) => a.group === groupName);
}

/** Option values for select/radio/checkbox come from the `options` validator. */
export function attributeOptions(
  attribute: UserProfileAttributeMetadata,
): string[] {
  const validator = attribute.validators.options as
    | { options?: string[] }
    | undefined;
  const options = validator?.options;
  return Array.isArray(options) ? options : [];
}

/** Narrow an annotation/validator value (`unknown`) to a string, else undefined. */
export function asString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

/**
 * react-hook-form value shape for the personal-info form: the four root
 * attributes plus custom attributes keyed by their (beer-escaped) name.
 * Single-valued attributes hold a string; multivalued hold string[].
 */
export type UserProfileFormValues = {
  username?: string;
  email?: string;
  firstName?: string;
  lastName?: string;
  attributes?: Record<string, string | string[]>;
};
