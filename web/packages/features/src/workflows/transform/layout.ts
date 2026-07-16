import ELK from 'elkjs/lib/elk.bundled.js';
import type { ElkExtendedEdge, ElkNode, LayoutOptions } from 'elkjs/lib/elk-api';
import { GRID, HEADER_ROW, LABEL_ROW } from '@pivox/ui/workflow';

import { ERROR_ROOT } from './ids';
import type { WorkflowGraph, WorkflowNode } from './ast-to-graph';

export { GRID } from '@pivox/ui/workflow';

// The bundled build runs the layout engine in-process (no web worker), so it
// works identically in the browser, SSR, and the test runner.
const elk = new ELK();

// ── Derived layout numbers (all GRID multiples) ────────────────────────────
// GRID + the node/row sizes are the single source of truth in `@pivox/ui`.
// Everything here is a GRID multiple, so with grid-multiple node sizes elk
// emits grid-aligned SIZES natively; only centering offsets can push a POSITION
// off-grid by a fraction of GRID, which the final snap corrects.
//
//   PAD          = 1×GRID = 20   content padding inside a container/region
//   V_GAP        = 3×GRID = 60   vertical gap between sequential steps
//   H_GAP        = 2×GRID = 40   horizontal gap between LTR regions (halved
//                                from the old 64 — the "too much space" fix)
//   CROSS_GAP    = 2×GRID = 40   cross-axis node spacing
//   CONTAINER_TOP = HEADER_ROW + PAD = 60   header bar (40) + content pad (20)
//   REGION_TOP    = LABEL_ROW  + PAD = 40   label strip (20) + content pad (20)
//   ERROR_GAP    = 4×GRID = 80   gap from the main flow to the on-error region
const PAD = GRID;
const V_GAP = 3 * GRID;
const H_GAP = 2 * GRID;
const CROSS_GAP = 2 * GRID;
const CONTAINER_TOP = HEADER_ROW + PAD;
const REGION_TOP = LABEL_ROW + PAD;
const ERROR_GAP = 4 * GRID;

// The main flow and every region run their steps top-to-bottom; a container
// (Condition/Parallel/Try) instead arranges its regions left-to-right. Both use
// `layered`; only direction, spacing, and padding differ. Every implied edge is
// intra-container (it links siblings), so each compound node lays out its own
// children independently (elk's default SEPARATE_CHILDREN).
const VERTICAL_OPTIONS: LayoutOptions = {
  'elk.algorithm': 'layered',
  'elk.direction': 'DOWN',
  'elk.layered.spacing.nodeNodeBetweenLayers': String(V_GAP),
  'elk.spacing.nodeNode': String(CROSS_GAP),
};

const REGION_LAYOUT_OPTIONS: LayoutOptions = {
  ...VERTICAL_OPTIONS,
  'elk.padding': `[top=${REGION_TOP},left=${PAD},bottom=${PAD},right=${PAD}]`,
};

// A container lays its regions in a horizontal row; `nodeNodeBetweenLayers` is
// the gap between adjacent regions. Synthetic ordering edges (added below) keep
// the regions in model order — otherwise/catch stay rightmost.
const CONTAINER_LAYOUT_OPTIONS: LayoutOptions = {
  'elk.algorithm': 'layered',
  'elk.direction': 'RIGHT',
  'elk.layered.spacing.nodeNodeBetweenLayers': String(H_GAP),
  'elk.spacing.nodeNode': String(CROSS_GAP),
  'elk.padding': `[top=${CONTAINER_TOP},left=${PAD},bottom=${PAD},right=${PAD}]`,
};

const ROOT_ID = '__root__';

/** Rounds a coordinate to the nearest grid line. */
const snap = (v: number): number => Math.round(v / GRID) * GRID;

/**
 * Assigns positions to every node and sizes to container/region nodes via elk's
 * hierarchical `layered` algorithm.
 *
 * Pure async function: the caller (hook layer) owns any memoization on version
 * identity. Child positions returned by elk are relative to the parent — which
 * is exactly React Flow's coordinate convention for parented nodes — so they map
 * back onto the graph without offset math.
 */
export async function layoutGraph(graph: WorkflowGraph): Promise<WorkflowGraph> {
  const nodeById = new Map(graph.nodes.map((node) => [node.id, node]));

  const childIds = new Map<string, string[]>();
  const roots: string[] = [];
  for (const node of graph.nodes) {
    if (node.parentId === undefined) {
      roots.push(node.id);
    } else {
      const siblings = childIds.get(node.parentId) ?? [];
      siblings.push(node.id);
      childIds.set(node.parentId, siblings);
    }
  }

  // Only sibling edges (both endpoints share a parent) drive the layout; each is
  // owned by that shared container (or the root graph for top-level steps). All
  // transform edges are sequential and thus already intra-container, but the
  // guard is kept so a future cross-hierarchy edge can't dangle an endpoint and
  // crash the layered algorithm.
  const edgesByContainer = new Map<string, ElkExtendedEdge[]>();
  for (const edge of graph.edges) {
    const sourceParent = nodeById.get(edge.source)?.parentId;
    const targetParent = nodeById.get(edge.target)?.parentId;
    if (sourceParent !== targetParent) continue;
    const container = sourceParent ?? ROOT_ID;
    const owned = edgesByContainer.get(container) ?? [];
    owned.push({ id: edge.id, sources: [edge.source], targets: [edge.target] });
    edgesByContainer.set(container, owned);
  }

  // Regions carry no edges between one another, so a plain layered pass would
  // stack them instead of rowing them and would not honor model order. A chain
  // of synthetic ordering edges (region[k] → region[k+1]) forces each region
  // into its own layer, left-to-right, so otherwise/catch stay rightmost. These
  // edges are elk-internal — they never reach React Flow.
  const orderingEdges = (containerId: string): ElkExtendedEdge[] => {
    const regions = childIds.get(containerId) ?? [];
    const edges: ElkExtendedEdge[] = [];
    for (let k = 0; k + 1 < regions.length; k += 1) {
      edges.push({
        id: `__order__:${regions[k]}=>${regions[k + 1]}`,
        sources: [regions[k]!],
        targets: [regions[k + 1]!],
      });
    }
    return edges;
  };

  // elk rejects nodes that carry `width`/`height`/`edges` keys set to
  // `undefined` (it reads them as null and crashes deep in the layered
  // algorithm), so each key is added only when it has a value. Compound nodes
  // omit size entirely — elk computes it from their children.
  const toElkNode = (id: string): ElkNode => {
    const node = nodeById.get(id);
    const children = childIds.get(id) ?? [];
    const elkNode: ElkNode = { id };
    if (children.length > 0) {
      const isContainer = node?.data.element === 'container';
      elkNode.layoutOptions = isContainer
        ? CONTAINER_LAYOUT_OPTIONS
        : REGION_LAYOUT_OPTIONS;
      elkNode.children = children.map(toElkNode);
      const edges = isContainer
        ? orderingEdges(id)
        : (edgesByContainer.get(id) ?? []);
      if (edges.length > 0) elkNode.edges = edges;
    } else {
      if (node?.width !== undefined) elkNode.width = node.width;
      if (node?.height !== undefined) elkNode.height = node.height;
      const edges = edgesByContainer.get(id);
      if (edges) elkNode.edges = edges;
    }
    return elkNode;
  };

  const elkRoot: ElkNode = {
    id: ROOT_ID,
    layoutOptions: VERTICAL_OPTIONS,
    children: roots.map(toElkNode),
  };
  const rootEdges = edgesByContainer.get(ROOT_ID);
  if (rootEdges) elkRoot.edges = rootEdges;

  const laidOut = await elk.layout(elkRoot);

  const positioned = new Map<string, { x: number; y: number; width: number; height: number }>();
  const collect = (elkNode: ElkNode): void => {
    for (const child of elkNode.children ?? []) {
      positioned.set(child.id, {
        x: child.x ?? 0,
        y: child.y ?? 0,
        width: child.width ?? 0,
        height: child.height ?? 0,
      });
      collect(child);
    }
  };
  collect(laidOut);

  // Snap every box to the grid. elk emits grid-aligned sizes from grid-aligned
  // inputs, so `snap` is a no-op on sizes in the common case; it corrects the
  // sub-grid centering offsets on positions. PAD (1×GRID) exceeds the ≤GRID/2
  // snap drift, so a child never leaves its parent's bounds.
  for (const [id, box] of positioned) {
    positioned.set(id, {
      x: snap(box.x),
      y: snap(box.y),
      width: snap(box.width),
      height: snap(box.height),
    });
  }

  // elk packs the disconnected "on error" component wherever it fits, which can
  // land it below or overlapping the main flow. Pin it to the right instead:
  // past the (grid-aligned) right edge of every top-level main-flow node,
  // top-aligned. Both operands are grid multiples, so the result stays snapped.
  const errorBox = positioned.get(ERROR_ROOT);
  if (errorBox) {
    let mainRight = 0;
    let mainTop = Number.POSITIVE_INFINITY;
    for (const id of roots) {
      if (id === ERROR_ROOT) continue;
      const box = positioned.get(id);
      if (!box) continue;
      mainRight = Math.max(mainRight, box.x + box.width);
      mainTop = Math.min(mainTop, box.y);
    }
    positioned.set(ERROR_ROOT, {
      ...errorBox,
      x: mainRight + ERROR_GAP,
      y: Number.isFinite(mainTop) ? mainTop : errorBox.y,
    });
  }

  const nodes: WorkflowNode[] = graph.nodes.map((node) => {
    const box = positioned.get(node.id);
    if (!box) return node;
    return {
      ...node,
      position: { x: box.x, y: box.y },
      width: box.width,
      height: box.height,
      style: { ...node.style, width: box.width, height: box.height },
    };
  });

  return { nodes, edges: graph.edges };
}
