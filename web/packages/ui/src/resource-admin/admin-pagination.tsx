'use client';

import { Button } from '@pivox/primitives/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@pivox/primitives/select';

import { PAGE_SIZES } from './use-list-controls';

/** Page-size selector plus a cursor pager (Previous/Next) for a resource-admin list. */
export function AdminPagination({
  pageSize,
  onPageSizeChange,
  hasPrevPage,
  hasNextPage,
  onPrev,
  onNext,
}: {
  pageSize: number;
  onPageSizeChange: (size: number) => void;
  hasPrevPage: boolean;
  hasNextPage: boolean;
  onPrev: () => void;
  onNext: () => void;
}) {
  return (
    <div className="flex items-center justify-end gap-4">
      <div className="flex items-center gap-2">
        <span className="text-sm text-muted-foreground">Rows per page</span>
        <Select
          value={String(pageSize)}
          onValueChange={(v) => onPageSizeChange(Number(v))}
        >
          <SelectTrigger className="w-[4.5rem]" aria-label="Rows per page">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((size) => (
              <SelectItem key={size} value={String(size)}>
                {size}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={!hasPrevPage}
          onClick={onPrev}
        >
          Previous
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={!hasNextPage}
          onClick={onNext}
        >
          Next
        </Button>
      </div>
    </div>
  );
}
