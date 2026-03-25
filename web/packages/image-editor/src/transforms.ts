import type { DragHandle } from './types';

/* ------------------------------------------------------------------ */
/*  Cursor mapping                                                    */
/* ------------------------------------------------------------------ */

export function handleToCursor(handle: DragHandle | null): string {
  switch (handle) {
    case 'nw':
    case 'se':
      return 'nwse-resize';
    case 'ne':
    case 'sw':
      return 'nesw-resize';
    case 'n':
    case 's':
      return 'ns-resize';
    case 'e':
    case 'w':
      return 'ew-resize';
    case 'move':
      return 'all-scroll';
    default:
      return 'default';
  }
}
