import { Button } from "@pivox/primitives/button";
import { Checkbox } from "@pivox/primitives/checkbox";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@pivox/primitives/field";
import { Input } from "@pivox/primitives/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@pivox/primitives/native-select";
import { RadioGroup, RadioGroupItem } from "@pivox/primitives/radio-group";
import { Textarea } from "@pivox/primitives/textarea";
import { Plus, X } from "lucide-react";
import {
  Controller,
  type Path,
  type RegisterOptions,
  type UseFormReturn,
} from "react-hook-form";

import {
  asString,
  attributeOptions,
  attributesForGroup,
  determineInputType,
  fieldName,
  type InputType,
  isRequiredAttribute,
  label,
  labelAttribute,
  orderedGroups,
  type ProfileMetadata,
  type UserProfileFormValues,
} from "./utils";

import type { UserProfileAttributeMetadata } from "@/user-profile/keycloak-types";
import type { TFunction } from "i18next";

type Attr = UserProfileAttributeMetadata;
type Annotations = Record<string, unknown>;
type Form = UseFormReturn<UserProfileFormValues>;
type FieldName = Path<UserProfileFormValues>;

type Props = {
  form: Form;
  metadata: ProfileMetadata;
  t: TFunction;
};

/** Renders every UP attribute, grouped, dynamically from the metadata. */
export function UserProfileFields({ form, metadata, t }: Props) {
  return (
    <div className="flex flex-col gap-8">
      {orderedGroups(metadata).map((group) => {
        const attrs = attributesForGroup(metadata, group.name);
        if (attrs.length === 0) return null;
        const heading = group.name
          ? label(t, group.displayHeader, group.name)
          : undefined;
        return (
          <section
            key={group.name ?? "__ungrouped"}
            className="flex flex-col gap-4"
          >
            {heading && (
              <div className="flex flex-col gap-1">
                <h2 className="text-base font-semibold">{heading}</h2>
                {group.displayDescription && (
                  <p className="text-sm text-muted-foreground">
                    {label(t, group.displayDescription, "")}
                  </p>
                )}
              </div>
            )}
            {attrs.map((attr) => (
              <AttributeField key={attr.name} attribute={attr} form={form} t={t} />
            ))}
          </section>
        );
      })}
    </div>
  );
}

function AttributeField({
  attribute,
  form,
  t,
}: {
  attribute: Attr;
  form: Form;
  t: TFunction;
}) {
  const annotations = (attribute.annotations ?? {});
  const name = fieldName(attribute.name);
  const inputType = determineInputType(attribute);
  const helper = asString(annotations.inputHelperTextBefore);
  const error = form.getFieldState(name, form.formState).error?.message;

  return (
    <Field>
      <FieldLabel htmlFor={attribute.name}>
        {labelAttribute(t, attribute)}
        {isRequiredAttribute(attribute) && (
          <span className="text-destructive"> *</span>
        )}
      </FieldLabel>
      {helper && <FieldDescription>{label(t, helper)}</FieldDescription>}
      <Control
        attribute={attribute}
        annotations={annotations}
        inputType={inputType}
        name={name}
        form={form}
        t={t}
      />
      {/* react-hook-form widened message to string | FieldError for nested errors. */}
      {typeof error === 'string' && <FieldError>{error}</FieldError>}
    </Field>
  );
}

type ControlProps = {
  attribute: Attr;
  annotations: Annotations;
  inputType: InputType;
  name: FieldName;
  form: Form;
  t: TFunction;
};

function Control({
  attribute,
  annotations,
  inputType,
  name,
  form,
  t,
}: ControlProps) {
  const readOnly = attribute.readOnly;

  // Multivalued free-text attributes render as an add/remove list.
  if (attribute.multivalued && !isOptionType(inputType)) {
    return <MultiInput attribute={attribute} name={name} form={form} t={t} />;
  }

  switch (inputType) {
    case "textarea":
      return (
        <Textarea
          id={attribute.name}
          disabled={readOnly}
          rows={numberAnnotation(annotations.inputTypeRows)}
          cols={numberAnnotation(annotations.inputTypeCols)}
          placeholder={placeholder(t, annotations)}
          {...form.register(name, registerOptions(attribute))}
        />
      );
    case "select":
    case "multiselect":
      return (
        <NativeSelect
          id={attribute.name}
          multiple={inputType === "multiselect"}
          disabled={readOnly}
          {...form.register(name, registerOptions(attribute))}
        >
          {inputType === "select" && <NativeSelectOption value="" />}
          {attributeOptions(attribute).map((o) => (
            <NativeSelectOption key={o} value={o}>
              {optionLabel(t, annotations, o)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      );
    case "select-radiobuttons":
      return (
        <Controller
          name={name}
          control={form.control}
          rules={registerOptions(attribute)}
          render={({ field }) => (
            <RadioGroup
              value={typeof field.value === "string" ? field.value : ""}
              onValueChange={field.onChange}
              disabled={readOnly}
              className="flex flex-col gap-2"
            >
              {attributeOptions(attribute).map((o) => (
                <label key={o} className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value={o} id={`${attribute.name}-${o}`} />
                  {optionLabel(t, annotations, o)}
                </label>
              ))}
            </RadioGroup>
          )}
        />
      );
    case "multiselect-checkboxes":
      return (
        <Controller
          name={name}
          control={form.control}
          rules={registerOptions(attribute)}
          render={({ field }) => {
            const selected = toArray(field.value);
            return (
              <div className="flex flex-col gap-2">
                {attributeOptions(attribute).map((o) => (
                  <label key={o} className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={selected.includes(o)}
                      disabled={readOnly}
                      onCheckedChange={(checked) => {
                        field.onChange(
                          checked
                            ? [...selected, o]
                            : selected.filter((v) => v !== o),
                        );
                      }}
                    />
                    {optionLabel(t, annotations, o)}
                  </label>
                ))}
              </div>
            );
          }}
        />
      );
    default:
      return (
        <Input
          id={attribute.name}
          type={htmlInputType(inputType)}
          disabled={readOnly}
          placeholder={placeholder(t, annotations)}
          {...rangeAttrs(attribute, inputType)}
          {...form.register(name, registerOptions(attribute))}
        />
      );
  }
}

/** A multivalued free-text list: N inputs with add/remove. */
function MultiInput({
  attribute,
  name,
  form,
  t,
}: {
  attribute: Attr;
  name: FieldName;
  form: Form;
  t: TFunction;
}) {
  const readOnly = attribute.readOnly;
  return (
    <Controller
      name={name}
      control={form.control}
      rules={registerOptions(attribute)}
      render={({ field }) => {
        const values = toArray(field.value);
        const rows = values.length > 0 ? values : [""];
        return (
          <div className="flex flex-col gap-2">
            {rows.map((v, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input
                  value={v}
                  disabled={readOnly}
                  onChange={(e) => {
                    field.onChange(
                      rows.map((row, idx) => (idx === i ? e.target.value : row)),
                    );
                  }}
                />
                {!readOnly && rows.length > 1 && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t("doRemove")}
                    onClick={() => {
                      field.onChange(rows.filter((_, idx) => idx !== i));
                    }}
                  >
                    <X className="size-4" />
                  </Button>
                )}
              </div>
            ))}
            {!readOnly && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="self-start"
                disabled={rows[rows.length - 1] === ""}
                onClick={() => {
                  field.onChange([...rows, ""]);
                }}
              >
                <Plus className="size-4" />
                {t("addMultivaluedLabel", { fieldLabel: labelAttribute(t, attribute) })}
              </Button>
            )}
          </div>
        );
      }}
    />
  );
}

/* ---- helpers ---- */

function isOptionType(inputType: InputType): boolean {
  return (
    inputType === "select" ||
    inputType === "multiselect" ||
    inputType === "select-radiobuttons" ||
    inputType === "multiselect-checkboxes"
  );
}

/** `html5-email` → `email`; everything else → `text`. */
function htmlInputType(inputType: InputType): string {
  return inputType.startsWith("html5-")
    ? inputType.slice("html5-".length)
    : "text";
}

function optionLabel(
  t: TFunction,
  annotations: Annotations,
  value: string,
): string {
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- KC UP attribute annotation `inputOptionLabels` is a string→string option-label map from the account REST metadata; annotations are typed `Record<string, unknown>` at this boundary.
  const labels = (annotations.inputOptionLabels ?? {}) as Record<string, string>;
  const prefix = asString(annotations.inputOptionLabelsI18nPrefix);
  return label(t, labels[value], value, prefix);
}

function placeholder(
  t: TFunction,
  annotations: Annotations,
): string | undefined {
  const raw = asString(annotations.inputTypePlaceholder);
  if (raw === undefined) return undefined;
  return label(t, raw, "", asString(annotations.inputOptionLabelsI18nPrefix));
}

function numberAnnotation(value: unknown): number | undefined {
  const n = typeof value === "number" ? value : Number(asString(value) ?? "");
  return Number.isFinite(n) ? n : undefined;
}

/** min/max for html5-number / html5-range from the numeric validators. */
function rangeAttrs(attribute: Attr, inputType: InputType) {
  if (inputType !== "html5-number" && inputType !== "html5-range") return {};
  const validators = attribute.validators;
  const integer = validators.integer as
    | { min?: number; max?: number }
    | undefined;
  const double = validators.double as
    | { min?: number; max?: number }
    | undefined;
  const bounds = integer ?? double;
  return {
    min: numberAnnotation(bounds?.min),
    max: numberAnnotation(bounds?.max),
  };
}

/** Client-side validation rules derived from KC validators + required. */
function registerOptions(
  attribute: Attr,
): RegisterOptions<UserProfileFormValues> {
  const rules: RegisterOptions<UserProfileFormValues> = {};
  if (attribute.required) rules.required = true;
  const validators = attribute.validators;
  const length = validators.length as
    | { min?: number; max?: number }
    | undefined;
  if (length?.min != null) rules.minLength = length.min;
  if (length?.max != null) rules.maxLength = length.max;
  const pattern = asString(
    (validators.pattern as { pattern?: unknown } | undefined)?.pattern,
  );
  if (pattern) {
    try {
      rules.pattern = new RegExp(pattern);
    } catch {
      // Invalid server-supplied pattern — skip the client rule; the server
      // validates on save regardless (don't crash the render).
    }
  }
  return rules;
}

function toArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((v): v is string => typeof v === "string");
  }
  const s = asString(value);
  return s ? [s] : [];
}
