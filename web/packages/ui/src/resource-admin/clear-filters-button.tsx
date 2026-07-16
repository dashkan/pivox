'use client';

import { Button } from '@pivox/primitives/button';
import { XIcon } from 'lucide-react';

/** Resets all active list filters in one action. */
export function ClearFiltersButton({ onClear }: { onClear: () => void }) {
  return (
    <Button variant="ghost" size="sm" onClick={onClear}>
      <XIcon />
      Clear filters
    </Button>
  );
}
