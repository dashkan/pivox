'use client';

import { Button } from '@pivox/primitives/button';
import { PencilIcon, Trash2Icon } from 'lucide-react';

import type { GridColumn } from '../grid';
import type { ResourceColumnContext } from './resource-list.context';

/**
 * Options for the {@link actionsColumn} factory. Affordances are presence-driven,
 * not toggled by descriptor flags: supply `delete` (with its confirm copy) to get
 * a delete button + the confirm dialog, set `edit` to get an edit button. This is
 * the one sanctioned boolean-opt exception the composition rules allow — even so,
 * `delete` itself is presence-driven via the opt object, so the create/delete
 * coupling never re-enters the descriptor.
 */
export interface ActionsColumnOptions<Row> {
  /** Render an Edit icon button that calls `ctx.onEdit(row)`. */
  edit?: boolean;
  /**
   * Render a Delete icon button that calls `ctx.openRemove(row)`. The `confirm`
   * copy is a param here — it exists only when the resource surfaces delete, and
   * `ResourceList.Root` reads it to render the confirm dialog. The confirm dialog
   * + the `remove` mutation stay a composite/descriptor concern; the button only
   * opens the confirm.
   */
  delete?: { confirm: (row: Row) => { title: string; description: string } };
  /** aria-label for the icon-only Edit button. */
  editLabel: string;
  /** aria-label for the icon-only Delete button. */
  removeLabel: string;
}

/**
 * Builds the trailing edit/delete affordance column for a resource list. It is
 * composed onto the view's content columns (columns-as-data, the sanctioned
 * carve-out) rather than baked into them — a resource gets edit+delete, edit-only,
 * or omits the column entirely. The cell reads the row the grid already hands the
 * renderer, so there is no new grid machinery: Edit navigates via `ctx.onEdit`,
 * Delete opens the confirm via `ctx.openRemove`.
 */
export function actionsColumn<Row, Extras>(
  ctx: ResourceColumnContext<Row, Extras>,
  opts: ActionsColumnOptions<Row>,
): GridColumn<Row> {
  const { onEdit, openRemove } = ctx;
  const { edit, delete: remove, editLabel, removeLabel } = opts;
  return {
    header: '',
    className: 'w-0',
    cell: (row) => (
      <div className="flex justify-end gap-1">
        {edit ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={editLabel}
            title={editLabel}
            onClick={() => onEdit(row)}
          >
            <PencilIcon />
          </Button>
        ) : null}
        {remove ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="text-destructive hover:text-destructive"
            aria-label={removeLabel}
            title={removeLabel}
            onClick={() => openRemove(row)}
          >
            <Trash2Icon />
          </Button>
        ) : null}
      </div>
    ),
  };
}
