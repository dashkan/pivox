'use client';

import { Button } from '@pivox/primitives/button';
import { PencilIcon, Trash2Icon } from 'lucide-react';

/**
 * Trailing edit/delete icon actions for a resource-admin table row. Both
 * buttons carry an `aria-label` (and a native tooltip) since they render icon
 * only; delete is styled with the destructive color.
 */
export function RowActions({
  editLabel,
  removeLabel,
  onEdit,
  onRemove,
}: {
  editLabel: string;
  removeLabel: string;
  onEdit: () => void;
  onRemove: () => void;
}) {
  return (
    <div className="flex justify-end gap-1">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={editLabel}
        title={editLabel}
        onClick={onEdit}
      >
        <PencilIcon />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="text-destructive hover:text-destructive"
        aria-label={removeLabel}
        title={removeLabel}
        onClick={onRemove}
      >
        <Trash2Icon />
      </Button>
    </div>
  );
}
