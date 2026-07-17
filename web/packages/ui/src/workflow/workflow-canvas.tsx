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
import { type CSSProperties, type MouseEvent, useCallback, useMemo, type ReactNode } from 'react';

import { useTheme } from '@/theme-switcher/use-theme';

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

// Re-skin the React Flow *chrome* (Background dots, Controls, MiniMap, edges,
// handles) with the app's shadcn design tokens instead of React Flow's stock
// palette. These NON-`-default` `--xy-*` variables are the ones React Flow's
// stylesheet reads first (`var(--xy-x, var(--xy-x-default))`) and never sets
// itself, so assigning them here always wins over the built-in light/dark
// defaults regardless of stylesheet order. Because every token
// (`var(--muted-foreground)` …) resolves against the document's `.dark` class,
// the chrome tracks light/dark automatically; `colorMode` keeps React Flow's
// own internal switch in agreement. Applied via a wrapper element so the
// overrides stay scoped to this canvas and never leak to other React Flow
// instances. The custom nodes/edge/panel are already tokenized (Tailwind
// `dark:`-aware shadcn classes), so only the chrome needs this.
const chromeTheme: CSSProperties & Record<`--${string}`, string> = {
  '--xy-background-pattern-color': 'var(--border)',
  '--xy-edge-stroke': 'var(--muted-foreground)',
  '--xy-edge-stroke-selected': 'var(--primary)',
  '--xy-connectionline-stroke': 'var(--muted-foreground)',
  '--xy-handle-background-color': 'var(--muted-foreground)',
  '--xy-handle-border-color': 'var(--background)',
  '--xy-controls-button-background-color': 'var(--card)',
  '--xy-controls-button-background-color-hover': 'var(--accent)',
  '--xy-controls-button-color': 'var(--card-foreground)',
  '--xy-controls-button-color-hover': 'var(--accent-foreground)',
  '--xy-controls-button-border-color': 'var(--border)',
  '--xy-minimap-background-color': 'var(--card)',
  '--xy-minimap-mask-background-color':
    'color-mix(in oklab, var(--background) 60%, transparent)',
  '--xy-minimap-node-background-color': 'var(--muted-foreground)',
};

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

  // Follow the app's theme choice (light/dark/system) — sourced from the
  // ThemeSwitcher's stored selection, NOT React Flow reading the OS directly,
  // so an explicit light/dark pick always wins. Both `system` values resolve
  // via matchMedia and therefore agree.
  const colorMode = useTheme();

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
    <div className="size-full" style={chromeTheme}>
      <Canvas
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        colorMode={colorMode}
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
    </div>
  );
}
