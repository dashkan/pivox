import type { EdgeProps, EdgeTypes } from '@xyflow/react';
import { BaseEdge, getBezierPath } from '@xyflow/react';
import type { ReactNode } from 'react';

// A single edge renderer registered as the `default` edge type; every transform
// edge is untyped, so React Flow routes them all here. Edges are strictly
// sequential (`start` entry + linear `sequence` links) and directional — the
// `markerEnd` supplied via the canvas's `defaultEdgeOptions` draws the arrowhead.

export function WorkflowEdgeRenderer({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
}: EdgeProps): ReactNode {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  return (
    <BaseEdge
      id={id}
      path={edgePath}
      markerEnd={markerEnd}
      className="stroke-muted-foreground/60"
    />
  );
}

/** React Flow edge-type map. Overrides the built-in `default` type. */
export const workflowEdgeTypes = {
  default: WorkflowEdgeRenderer,
} satisfies EdgeTypes;
