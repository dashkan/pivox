'use client';

import { Button } from '@pivox/primitives/button';
import { Field, FieldDescription, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';

/**
 * Read-only "Identifier" that auto-derives from the display name, with an
 * "Edit" affordance to override. Once editing, the parent stops re-deriving
 * (it flips its own "touched" flag). Shared by the connector and secret create
 * forms — the identifier is immutable, so this is never shown in edit mode.
 */
export function IdentifierField({
  label,
  value,
  editing,
  onEdit,
  onChange,
  disabled,
}: {
  label: string;
  value: string;
  editing: boolean;
  onEdit: () => void;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      {editing ? (
        <Input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          disabled={disabled}
        />
      ) : (
        <div className="flex items-center justify-between gap-2 rounded-md border px-3 py-2">
          <code className="truncate text-sm">{value || '—'}</code>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onEdit}
            disabled={disabled}
          >
            Edit
          </Button>
        </div>
      )}
      <FieldDescription>
        Permanent. Lowercase letters, numbers, and hyphens.
      </FieldDescription>
    </Field>
  );
}
