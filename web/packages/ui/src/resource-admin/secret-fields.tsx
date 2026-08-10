'use client';

import { Button } from '@pivox/primitives/button';
import { Checkbox } from '@pivox/primitives/checkbox';
import { Field, FieldDescription, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import { EyeIcon, EyeOffIcon } from 'lucide-react';
import { useState } from 'react';

import { IdentifierField } from './identifier-field';
import { KeyValueEditor } from './key-value-editor';
import { ScopeSelect } from './scope-select';
import { spaceLabel } from './connector-shared';
import { useSecretForm } from './secret-form.context';
import { slugify } from './slug';

/**
 * Secret fields split into EXPLICIT create/edit variants
 * (`patterns-explicit-variants`), the secret twin of the connector fields. They
 * read the resource-owned `SecretFormContext` for `values` / `patch`;
 * `FormPage.Submit` reads the generic context for `canSubmit`.
 *
 * The value is write-only: the API never returns it, so the list/edit views only
 * ever WRITE it — masked input, eye-reveal toggle, and password-manager opt-outs
 * carried over verbatim from the old dialog form.
 */

/**
 * The masked, write-only value input + eye-reveal toggle. Shared by create
 * (always shown) and edit (only when rotating).
 */
function SecretValueField({ label }: { label: string }) {
  const { values, patch } = useSecretForm();
  // The value input is masked by default; the eye toggle reveals it.
  const [showValue, setShowValue] = useState(false);
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <div className="relative">
        {/* A `password`-typed field (or a name/id containing "password")
            triggers browser/extension password managers. The value is
            write-only and not a login credential, so opt every known manager
            out and keep the field name neutral. */}
        <Input
          type={showValue ? 'text' : 'password'}
          id="secret-value"
          name="secret-value"
          autoComplete="off"
          data-1p-ignore="true"
          data-lpignore="true"
          data-bwignore="true"
          value={values.value}
          onChange={(e) => patch({ value: e.target.value })}
          className="pr-9"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={showValue ? 'Hide value' : 'Show value'}
          title={showValue ? 'Hide value' : 'Show value'}
          onClick={() => setShowValue((shown) => !shown)}
          className="absolute inset-y-0 end-1 my-auto text-muted-foreground"
        >
          {showValue ? <EyeOffIcon /> : <EyeIcon />}
        </Button>
      </div>
      <FieldDescription>
        Write-only. The value is never shown again — store multi-field
        credentials as a JSON blob.
      </FieldDescription>
    </Field>
  );
}

/** The annotations map editor, shared by both variants. */
function SecretAnnotationsField() {
  const { values, patch } = useSecretForm();
  return (
    <KeyValueEditor
      label="Annotations"
      keyPlaceholder="env"
      valuePlaceholder="prod"
      entries={values.annotations}
      onChange={(annotations) => patch({ annotations })}
    />
  );
}

/**
 * Create field-set: editable display name (auto-derives the identifier until the
 * user overrides it), the immutable identifier, an editable scope PICKER (org
 * rollup — the parent isn't decided yet), the required value, then annotations.
 * Scope is just a value in the resource form context, so `submit()` reads it the
 * same whether it came from this picker or (future) a route param.
 */
export function SecretCreateFields() {
  const { values, patch, spaceOptions } = useSecretForm();
  // Local UI toggles: until the user overrides the id, it tracks the display
  // name; once editing, the parent stops re-deriving.
  const [idTouched, setIdTouched] = useState(false);
  const [editingId, setEditingId] = useState(false);

  const updateDisplayName = (displayName: string) => {
    patch({
      displayName,
      secretId: idTouched ? values.secretId : slugify(displayName),
    });
  };

  return (
    <>
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input
          value={values.displayName}
          onChange={(e) => updateDisplayName(e.target.value)}
        />
      </Field>
      <IdentifierField
        label="Identifier"
        value={values.secretId}
        editing={editingId}
        onEdit={() => {
          setEditingId(true);
          setIdTouched(true);
        }}
        onChange={(secretId) => {
          setIdTouched(true);
          patch({ secretId });
        }}
      />
      <Field>
        <FieldLabel>Scope</FieldLabel>
        <ScopeSelect
          value={values.scope}
          spaces={spaceOptions}
          onChange={(scope) => patch({ scope })}
          allLabel="Organization (no space)"
          placeholder="No space — organization"
        />
      </Field>
      <SecretValueField label="Value" />
      <SecretAnnotationsField />
    </>
  );
}

/**
 * Edit field-set: plain display name (no identifier to derive), the immutable
 * scope shown read-only (a secret can't move between org and space), a "Rotate
 * value" toggle that reveals the write-only value input, then annotations. No
 * identifier field — the identifier is permanent.
 */
export function SecretEditFields() {
  const { values, patch, record, spaceOptions } = useSecretForm();
  return (
    <>
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input
          value={values.displayName}
          onChange={(e) => patch({ displayName: e.target.value })}
        />
      </Field>
      <Field>
        <FieldLabel>Scope</FieldLabel>
        {/* Immutable — a secret can't move between org and space. */}
        <Input
          value={spaceLabel(record?.name, spaceOptions)}
          readOnly
          disabled
        />
      </Field>
      <div className="flex items-center gap-2 text-sm">
        <Checkbox
          id="secret-rotate"
          checked={values.rotate}
          onCheckedChange={(checked) =>
            patch({ rotate: checked, value: '' })
          }
        />
        <label htmlFor="secret-rotate" className="cursor-pointer">
          Rotate value
        </label>
      </div>
      {values.rotate ? <SecretValueField label="New value" /> : null}
      <SecretAnnotationsField />
    </>
  );
}
