import { cn } from '@pivox/primitives/utils';
import type { Node as FlowNode, NodeProps, NodeTypes } from '@xyflow/react';
import type { ReactNode } from 'react';

import type { RegionNodeData } from '../graph-types';
import { CONTENT_PAD, LABEL_ROW } from '../grid';

// Region nodes are React Flow parent (group) nodes: a labeled, bordered box that
// their child steps nest inside via `parentId`. They are never edge endpoints.
// The label strip is exactly LABEL_ROW tall — matching the elk top padding so
// the first child clears it.

type RegionNodeType<K extends RegionNodeData['kind']> = FlowNode<RegionNodeData, K>;

type RegionShellProps = {
  label: string;
  className?: string;
};

function RegionShell({ label, className }: RegionShellProps): ReactNode {
  return (
    <div
      className={cn(
        'size-full overflow-hidden rounded-md border border-dashed bg-muted/30',
        className,
      )}
    >
      <div
        className="absolute top-0 left-0 flex w-full items-center"
        style={{ height: LABEL_ROW, paddingInline: CONTENT_PAD }}
      >
        <span className="truncate text-xs font-medium text-muted-foreground">{label}</span>
      </div>
    </div>
  );
}

export function BranchRegionNode({ data }: NodeProps<RegionNodeType<'branch'>>): ReactNode {
  return <RegionShell label={data.when ? `when ${data.when}` : 'when'} />;
}

export function OtherwiseRegionNode(_: NodeProps<RegionNodeType<'otherwise'>>): ReactNode {
  return <RegionShell label="else" />;
}

export function LaneRegionNode(_: NodeProps<RegionNodeType<'lane'>>): ReactNode {
  return <RegionShell label="lane" />;
}

export function TryBodyRegionNode(_: NodeProps<RegionNodeType<'try-body'>>): ReactNode {
  return <RegionShell label="try" />;
}

export function TryCatchRegionNode(_: NodeProps<RegionNodeType<'try-catch'>>): ReactNode {
  return <RegionShell label="catch" />;
}

export function ErrorRegionNode(_: NodeProps<RegionNodeType<'error'>>): ReactNode {
  return (
    <RegionShell
      label="on error"
      className="border-destructive/40 bg-destructive/5"
    />
  );
}

/** React Flow node-type map for the six region kinds. */
export const regionNodeTypes = {
  branch: BranchRegionNode,
  otherwise: OtherwiseRegionNode,
  lane: LaneRegionNode,
  'try-body': TryBodyRegionNode,
  'try-catch': TryCatchRegionNode,
  error: ErrorRegionNode,
} satisfies NodeTypes;
