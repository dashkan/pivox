'use client';

import { TableCell, TableRow } from '@pivox/primitives/table';

/**
 * Page chrome shared by the resource admin surfaces: a title/description header
 * with a composed header-actions slot (the top-right primary action area), over a
 * content slot (the table). The frame no longer knows about "New" — the create
 * affordance is composed into `actions` (`ResourceList.Toolbar` /
 * `ResourceList.NewButton`), so a create-less resource simply composes nothing
 * and the header renders no action.
 */
export function AdminFrame({
  title,
  description,
  actions,
  children,
}: {
  title: string;
  description: string;
  /** Composed header-actions (top-right), e.g. the New button. Omit for none. */
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-1 flex-col gap-6 p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
        {actions}
      </div>
      {children}
    </div>
  );
}

/** Centered state for empty / loading / error content regions. */
export function AdminNotice({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-1 items-center justify-center rounded-lg border border-dashed p-10 text-sm text-muted-foreground">
      {children}
    </div>
  );
}

/**
 * Full-width table-body row for loading / error / empty states, so the header
 * and filter row above it stay mounted while the body swaps.
 */
export function AdminNoticeRow({
  colSpan,
  children,
}: {
  colSpan: number;
  children: React.ReactNode;
}) {
  return (
    <TableRow>
      <TableCell
        colSpan={colSpan}
        className="h-24 text-center text-sm text-muted-foreground"
      >
        {children}
      </TableCell>
    </TableRow>
  );
}
