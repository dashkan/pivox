'use client';

import { Button } from '@pivox/primitives/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@pivox/primitives/select';

import { PAGE_SIZES } from './page-sizes';

/**
 * "Rows per page" selector. Pure presentational (props in, callback out). Shared
 * across pagination strategies — a future numbered pager reuses it untouched, so
 * it lives here rather than being bound to the cursor pager.
 */
export function PageSizeSelect({
  pageSize,
  onPageSizeChange,
}: {
  pageSize: number;
  onPageSizeChange: (size: number) => void;
}) {
  return (
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
  );
}

/**
 * Previous/Next cursor pager. Pure presentational. The `hasPrev`/`hasNext`
 * booleans are opaque to the pager (cursor or offset — it can't tell); the
 * consumer decides when an adjacent page exists.
 */
export function CursorPager({
  hasPrev,
  hasNext,
  onPrev,
  onNext,
}: {
  hasPrev: boolean;
  hasNext: boolean;
  onPrev: () => void;
  onNext: () => void;
}) {
  return (
    <div className="flex gap-2">
      <Button variant="outline" size="sm" disabled={!hasPrev} onClick={onPrev}>
        Previous
      </Button>
      <Button variant="outline" size="sm" disabled={!hasNext} onClick={onNext}>
        Next
      </Button>
    </div>
  );
}
