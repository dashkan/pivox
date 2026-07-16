import { Handle, type Node as FlowNode, type NodeProps, type NodeTypes, Position } from '@xyflow/react';
import { Play } from 'lucide-react';
import type { ReactNode } from 'react';

import type { StartNodeData } from '../graph-types';

// Synthetic entry marker at the top of the flow. Source-only (Bottom): it feeds
// the first root step and is never an edge target. Carries no config.

type StartNodeType = FlowNode<StartNodeData, 'start'>;

export function StartNode(_: NodeProps<StartNodeType>): ReactNode {
  return (
    <div className="flex size-full items-center justify-center gap-1.5 rounded-full border bg-secondary px-3 py-1.5">
      <Play className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
      <span className="text-xs font-medium">Start</span>
      <Handle position={Position.Bottom} type="source" />
    </div>
  );
}

/** React Flow node-type map for the synthetic start marker. */
export const startNodeTypes = {
  start: StartNode,
} satisfies NodeTypes;
