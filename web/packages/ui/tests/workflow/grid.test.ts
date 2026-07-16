import { describe, expect, it } from 'vitest';

import {
  ACTIVITY_NODE_HEIGHT,
  ACTIVITY_NODE_HEIGHT_COMPACT,
  CONTENT_PAD,
  CONTENT_ROW,
  GRID,
  HEADER_ROW,
  LABEL_ROW,
  NODE_WIDTH,
  START_HEIGHT,
  START_WIDTH,
} from '../../src/workflow/grid';

// The interior rows must each land on the grid so a node's total height is a
// GRID multiple BY CONSTRUCTION (header + content), not by padding a mismatched
// interior. This test is the machine-checked version of grid.ts's doc block.
describe('workflow grid', () => {
  it('makes every size and internal row an exact multiple of GRID', () => {
    for (const value of [
      NODE_WIDTH,
      HEADER_ROW,
      CONTENT_ROW,
      LABEL_ROW,
      CONTENT_PAD,
      ACTIVITY_NODE_HEIGHT,
      ACTIVITY_NODE_HEIGHT_COMPACT,
      START_WIDTH,
      START_HEIGHT,
    ]) {
      expect(value % GRID).toBe(0);
    }
  });

  it('derives the activity node height from its header + content rows', () => {
    // Total height is the sum of grid-aligned rows, so it is grid-aligned too.
    expect(ACTIVITY_NODE_HEIGHT).toBe(HEADER_ROW + CONTENT_ROW);
    // The compact (summary-less) node is header-only.
    expect(ACTIVITY_NODE_HEIGHT_COMPACT).toBe(HEADER_ROW);
  });

  it('pins the documented base numbers', () => {
    expect(GRID).toBe(20);
    expect(NODE_WIDTH).toBe(400);
    expect(HEADER_ROW).toBe(40);
    expect(CONTENT_ROW).toBe(40);
    expect(LABEL_ROW).toBe(20);
    expect(CONTENT_PAD).toBe(20);
  });
});
