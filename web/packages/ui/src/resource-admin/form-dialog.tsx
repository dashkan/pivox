'use client';

import { Button } from '@pivox/primitives/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@pivox/primitives/dialog';
import { FieldError } from '@pivox/primitives/field';

/**
 * Dialog shell for a create/edit form. The resource-specific form (fields +
 * submit handling) is passed as `children` inside the caller's own `<form>`,
 * so this component owns only the modal chrome — no field state, no submit
 * logic.
 */
export function FormDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        {children}
      </DialogContent>
    </Dialog>
  );
}

/**
 * Submit/cancel footer + error line shared by both resource forms.
 *
 * `pending` (a write is in flight) and `canSubmit` (the form is valid) are
 * separate concerns: only `pending` gates Cancel, and only `pending` drives the
 * "Saving…" label — a merely-invalid form must not disable Cancel or read as
 * saving.
 */
export function FormActions({
  error,
  pending,
  canSubmit,
  submitLabel,
  onCancel,
}: {
  error: string | null;
  pending: boolean;
  canSubmit: boolean;
  submitLabel: string;
  onCancel: () => void;
}) {
  return (
    <>
      {error && <FieldError>{error}</FieldError>}
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={onCancel}
          disabled={pending}
        >
          Cancel
        </Button>
        <Button type="submit" disabled={pending || !canSubmit}>
          {pending ? 'Saving…' : submitLabel}
        </Button>
      </DialogFooter>
    </>
  );
}
