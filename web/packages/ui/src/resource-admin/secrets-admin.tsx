'use client';

import { parseResourceName } from '@pivox/client';
import { Button } from '@pivox/primitives/button';
import { Checkbox } from '@pivox/primitives/checkbox';
import { Field, FieldDescription, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@pivox/primitives/table';
import { EyeIcon, EyeOffIcon } from 'lucide-react';
import { useState } from 'react';

import { AdminFrame, AdminNotice } from './admin-frame';
import { DeleteDialog } from './delete-dialog';
import { FormActions, FormDialog } from './form-dialog';
import { IdentifierField } from './identifier-field';
import { KeyValueEditor } from './key-value-editor';
import { actorLabel, formatTimestamp } from './meta-cells';
import { RowActions } from './row-actions';
import { SecretsAdminContext, useSecretsAdmin } from './secrets-admin.context';
import { isValidIdentifier, slugify } from './slug';

import type {
  KeyValueEntry,
  Secret,
  SecretFormValues,
  SecretsAdminContextValue,
} from './types';

function leafId(name: string | undefined): string {
  if (!name) return '';
  const segments = parseResourceName(name);
  return segments.secrets ?? '';
}

function SecretsAdminProvider({
  value,
  children,
}: {
  value: SecretsAdminContextValue;
  children: React.ReactNode;
}) {
  return <SecretsAdminContext value={value}>{children}</SecretsAdminContext>;
}

function annotationsToEntries(
  annotations: Record<string, string> | undefined,
): KeyValueEntry[] {
  return Object.entries(annotations ?? {}).map(([key, value]) => ({
    key,
    value,
  }));
}

function SecretForm() {
  const { state, actions } = useSecretsAdmin();
  const { dialog } = state;
  const editing = dialog.editing;
  const isCreate = dialog.mode === 'create';

  const [values, setValues] = useState<SecretFormValues>(() => ({
    secretId: '',
    displayName: editing?.displayName ?? '',
    annotations: annotationsToEntries(editing?.annotations),
    value: '',
    rotate: isCreate,
  }));
  // Until the user overrides the id, it auto-derives from the display name.
  const [idTouched, setIdTouched] = useState(false);
  const [editingId, setEditingId] = useState(false);
  // The value input is masked by default; the eye toggle reveals it.
  const [showValue, setShowValue] = useState(false);

  const patch = (next: Partial<SecretFormValues>) => {
    setValues((v) => ({ ...v, ...next }));
  };

  const updateDisplayName = (displayName: string) => {
    setValues((v) => ({
      ...v,
      displayName,
      secretId: idTouched ? v.secretId : slugify(displayName),
    }));
  };

  // The value is required on create and whenever an edit rotates it.
  const valueRequired = isCreate || values.rotate;
  const canSubmit =
    (!isCreate || isValidIdentifier(values.secretId)) &&
    (!valueRequired || values.value.length > 0);

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        actions.submit(values);
      }}
    >
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input
          value={values.displayName}
          onChange={(e) => updateDisplayName(e.target.value)}
          disabled={dialog.pending}
        />
      </Field>
      {isCreate && (
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
          disabled={dialog.pending}
        />
      )}

      {!isCreate && (
        <div className="flex items-center gap-2 text-sm">
          <Checkbox
            id="secret-rotate"
            checked={values.rotate}
            onCheckedChange={(checked) =>
              patch({ rotate: checked === true, value: '' })
            }
            disabled={dialog.pending}
          />
          <label htmlFor="secret-rotate" className="cursor-pointer">
            Rotate value
          </label>
        </div>
      )}

      {valueRequired && (
        <Field>
          <FieldLabel>{isCreate ? 'Value' : 'New value'}</FieldLabel>
          <div className="relative">
            {/* A `password`-typed field (or a name/id containing "password")
                triggers browser/extension password managers. The value is
                write-only and not a login credential, so opt every known
                manager out and keep the field name neutral. */}
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
              disabled={dialog.pending}
              className="pr-9"
            />
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={showValue ? 'Hide value' : 'Show value'}
              title={showValue ? 'Hide value' : 'Show value'}
              onClick={() => setShowValue((shown) => !shown)}
              disabled={dialog.pending}
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
      )}

      <KeyValueEditor
        label="Annotations"
        keyPlaceholder="env"
        valuePlaceholder="prod"
        entries={values.annotations}
        onChange={(annotations) => patch({ annotations })}
        disabled={dialog.pending}
      />

      <FormActions
        error={dialog.error}
        pending={dialog.pending}
        canSubmit={canSubmit}
        submitLabel={isCreate ? 'Create secret' : 'Save changes'}
        onCancel={actions.closeDialog}
      />
    </form>
  );
}

function SecretsTable() {
  const { state, actions } = useSecretsAdmin();
  const { secrets } = state;

  if (state.isLoading) {
    return <AdminNotice>Loading secrets…</AdminNotice>;
  }
  if (state.loadError) {
    return <AdminNotice>{state.loadError}</AdminNotice>;
  }
  if (secrets.length === 0) {
    return <AdminNotice>No secrets yet.</AdminNotice>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Created</TableHead>
          <TableHead>Updated</TableHead>
          <TableHead className="w-0" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {secrets.map((secret: Secret) => (
          <TableRow key={secret.name}>
            <TableCell className="font-medium">
              {secret.displayName || leafId(secret.name)}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {formatTimestamp(secret.createTime)} ·{' '}
              {actorLabel(secret.createdBy)}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {formatTimestamp(secret.updateTime)} ·{' '}
              {actorLabel(secret.updatedBy)}
            </TableCell>
            <TableCell>
              <RowActions
                editLabel="Edit secret"
                removeLabel="Delete secret"
                onEdit={() => actions.openEdit(secret)}
                onRemove={() => actions.openRemove(secret)}
              />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function SecretsAdminRoot() {
  const { state, actions } = useSecretsAdmin();
  const { dialog, remove } = state;

  return (
    <>
      <AdminFrame
        title="Secrets"
        description="Encrypted credentials resolved by connectors at request time. Values are write-only."
        newLabel="New secret"
        onNew={actions.openCreate}
      >
        <SecretsTable />
      </AdminFrame>

      <FormDialog
        open={dialog.open}
        onOpenChange={(open) => {
          if (!open) actions.closeDialog();
        }}
        title={dialog.mode === 'create' ? 'New secret' : 'Edit secret'}
        description={
          dialog.mode === 'edit'
            ? 'Update metadata. Tick “Rotate value” to replace the stored value.'
            : undefined
        }
      >
        <SecretForm key={dialog.editing?.name ?? 'new'} />
      </FormDialog>

      <DeleteDialog
        open={remove.target !== null}
        onOpenChange={(open) => {
          if (!open) actions.closeRemove();
        }}
        title="Delete secret?"
        description={`This permanently deletes "${
          remove.target?.displayName || leafId(remove.target?.name)
        }". A secret still referenced by a connector can't be deleted.`}
        error={remove.error}
        pending={remove.pending}
        onConfirm={actions.confirmRemove}
      />
    </>
  );
}

export const SecretsAdmin = {
  Provider: SecretsAdminProvider,
  Root: SecretsAdminRoot,
};
