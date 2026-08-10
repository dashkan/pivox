'use client';

import { TableHead } from '@pivox/primitives/table';
import { ArrowDownIcon, ArrowUpIcon, ChevronsUpDownIcon } from 'lucide-react';

import type { SortState } from './types';

const SORT_ICON = {
  asc: ArrowUpIcon,
  desc: ArrowDownIcon,
} as const;

const ARIA_SORT = {
  asc: 'ascending',
  desc: 'descending',
} as const;

/**
 * A column header that drives a server-side sort. Renders the active direction
 * arrow (or a neutral toggle glyph) and reports it via `aria-sort`; clicking
 * cycles the column through the parent's `toggleSort`.
 */
export function SortableHeader({
  field,
  sort,
  onToggle,
  children,
  className,
}: {
  field: string;
  sort: SortState | null;
  onToggle: (field: string) => void;
  children: React.ReactNode;
  className?: string;
}) {
  const direction = sort?.field === field ? sort.direction : null;
  const Icon = direction ? SORT_ICON[direction] : ChevronsUpDownIcon;

  return (
    <TableHead
      className={className}
      aria-sort={direction ? ARIA_SORT[direction] : 'none'}
    >
      <button
        type="button"
        onClick={() => onToggle(field)}
        className="-mx-1 inline-flex items-center gap-1 rounded px-1 hover:text-foreground"
      >
        {children}
        <Icon
          className={
            direction ? 'size-3.5' : 'size-3.5 text-muted-foreground/50'
          }
        />
      </button>
    </TableHead>
  );
}
