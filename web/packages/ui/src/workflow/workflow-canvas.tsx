'use client';

import { Canvas } from '@pivox/primitives/canvas';
import { Controls } from '@pivox/primitives/controls';
import {
  type DefaultEdgeOptions,
  type FitViewOptions,
  MarkerType,
  MiniMap,
  type Node,
  type NodeTypes,
  type ProOptions,
} from '@xyflow/react';
import { type MouseEvent, useCallback, useMemo, type ReactNode } from 'react';

import { ConfigPanel } from './config-panel';
import type { WorkflowEdge, WorkflowNode } from './graph-types';
import { GRID } from './grid';
import { activityNodeTypes } from './nodes/activity-node';
import { regionNodeTypes } from './nodes/branch-node';
import { containerNodeTypes } from './nodes/container-node';
import { startNodeTypes } from './nodes/start-node';
import type { RunState } from './run-status';
import { SelectionProvider, useSelection } from './selection-context';
import { workflowEdgeTypes } from './workflow-edge';

// Stable module-level references — React Flow re-mounts every node/edge when
// these identities change, so they MUST NOT be re-created per render.
const nodeTypes = {
  ...activityNodeTypes,
  ...containerNodeTypes,
  ...regionNodeTypes,
  ...startNodeTypes,
} satisfies NodeTypes;
const edgeTypes = workflowEdgeTypes;

// Suppress the React Flow attribution watermark.
const proOptions: ProOptions = { hideAttribution: true };

// Every edge is directional: draw an arrowhead at the target end.
const defaultEdgeOptions: DefaultEdgeOptions = {
  markerEnd: { type: MarkerType.ArrowClosed, width: 18, height: 18 },
};

// Allow zooming out far enough to survey a big workflow, though the initial
// view (below) opens centered on Start at 1:1, not fit-to-graph.
const MIN_ZOOM = 0.1;

// Snap dragged nodes to the grid (Phase 3 editing); the layout already emits
// grid-aligned positions, so read-only nodes are on-grid regardless.
const snapGrid: [number, number] = [GRID, GRID];

export type WorkflowCanvasProps = {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onNodeSelect?: (node: WorkflowNode | undefined) => void;
  /**
   * Per-step run state keyed by AST step path, for the T7 monitor overlay.
   * Accepted now as the wiring seam; the definition view does not color by it.
   */
  runState?: Record<string, RunState>;
};

export function WorkflowCanvas(props: WorkflowCanvasProps): ReactNode {
  return (
    <SelectionProvider>
      <CanvasInner {...props} />
    </SelectionProvider>
  );
}

function CanvasInner({ nodes, edges, onNodeSelect }: WorkflowCanvasProps): ReactNode {
  const { select } = useSelection();

  // Open centered on the Start node at 1:1 rather than fitting the whole graph
  // (a big workflow would zoom out too far to read). Falls back to fit-all only
  // when there is no Start node (e.g. an empty definition).
  const startId = nodes.find((n) => n.data.element === 'start')?.id;
  const fitViewOptions = useMemo<FitViewOptions>(
    () => (startId ? { nodes: [{ id: startId }], minZoom: 1, maxZoom: 1 } : { maxZoom: 1 }),
    [startId],
  );

  const handleNodeClick = useCallback(
    (_: MouseEvent, node: Node) => {
      select(node.id);
      onNodeSelect?.(nodes.find((n) => n.id === node.id));
    },
    [nodes, select, onNodeSelect],
  );

  const handlePaneClick = useCallback(() => {
    select(undefined);
    onNodeSelect?.(undefined);
  }, [select, onNodeSelect]);

  return (
    <Canvas
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      nodesDraggable={false}
      nodesConnectable={false}
      fitView
      fitViewOptions={fitViewOptions}
      minZoom={MIN_ZOOM}
      snapToGrid
      snapGrid={snapGrid}
      defaultEdgeOptions={defaultEdgeOptions}
      proOptions={proOptions}
      onNodeClick={handleNodeClick}
      onPaneClick={handlePaneClick}
    >
      <Controls showInteractive={false} />
      <MiniMap pannable zoomable />
      <ConfigPanel />
    </Canvas>
  );
}
