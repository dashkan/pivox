'use client';

import { Button } from '@pivox/primitives/button';
import { FilterIcon } from 'lucide-react';

/** Toolbar button that shows/hides a list's filter row. */
export function FilterToggleButton({
  active,
  onToggle,
}: {
  active: boolean;
  onToggle: () => void;
}) {
  return (
    <Button
      variant={active ? 'default' : 'outline'}
      size="sm"
      aria-pressed={active}
      onClick={onToggle}
    >
      <FilterIcon />
      Filter
    </Button>
  );
}
