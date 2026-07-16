// ── The workflow-canvas grid — single source of truth ──────────────────────
//
// Every size, row height, gap, padding, and position on the canvas derives from
// GRID. Change GRID and the whole diagram rescales while staying grid-aligned.
// Nothing in the workflow UI may hardcode a pixel value that isn't a GRID
// multiple built from these constants.
//
//   GRID                          = 20px      base unit
//
//   Node outer sizes (fed to elk, all GRID multiples):
//   NODE_WIDTH                    = 20×GRID = 400   fixed activity-node width
//   ACTIVITY_NODE_HEIGHT          =  4×GRID =  80   activity node WITH a summary
//   ACTIVITY_NODE_HEIGHT_COMPACT  =  2×GRID =  40   activity node WITHOUT one (end)
//   START_WIDTH                   =  6×GRID = 120
//   START_HEIGHT                  =  2×GRID =  40
//
//   Internal rows (each a GRID multiple, so a node's height is a GRID multiple
//   BY CONSTRUCTION: header + content sums to a GRID multiple):
//   HEADER_ROW                    =  2×GRID =  40   activity + container header
//   CONTENT_ROW                   =  2×GRID =  40   activity summary row
//   LABEL_ROW                     =  1×GRID =  20   region label strip
//   CONTENT_PAD                   =  1×GRID =  20   horizontal padding inside a row
//     → ACTIVITY_NODE_HEIGHT = HEADER_ROW + CONTENT_ROW (40 + 40 = 80)
//     → ACTIVITY_NODE_HEIGHT_COMPACT = HEADER_ROW (40)
//
// Layout padding + inter-node spacing (also GRID multiples) are derived in
// `@pivox/features` `transform/layout.ts`, which imports GRID/HEADER_ROW/
// LABEL_ROW from here so the container header/label heights and the elk top
// padding agree.

export const GRID = 20;

export const NODE_WIDTH = 20 * GRID;
export const HEADER_ROW = 2 * GRID;
export const CONTENT_ROW = 2 * GRID;
export const LABEL_ROW = 1 * GRID;
export const CONTENT_PAD = 1 * GRID;

export const ACTIVITY_NODE_HEIGHT = HEADER_ROW + CONTENT_ROW;
export const ACTIVITY_NODE_HEIGHT_COMPACT = HEADER_ROW;

export const START_WIDTH = 6 * GRID;
export const START_HEIGHT = 2 * GRID;
