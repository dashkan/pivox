import { cn } from '@pivox/primitives/utils';
import { Handle, type Node as FlowNode, type NodeProps, type NodeTypes, Position } from '@xyflow/react';
import { GitBranch, ShieldAlert, Split, type LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';

import type { ContainerNodeData } from '../graph-types';
import { CONTENT_PAD, HEADER_ROW } from '../grid';
import { useSelection } from '../selection-context';

// Container nodes (Condition/Parallel/Try) are React Flow parent (group) nodes
// wrapping their region children. They sit in a sequence, so they carry a Top
// target for the incoming `sequence` edge and a Bottom source for the outgoing
// one. The header bar is exactly HEADER_ROW tall — matching the elk top padding
// so the first region child clears it.

type ContainerNodeType<K extends ContainerNodeData['kind']> = FlowNode<ContainerNodeData, K>;

type ContainerShellProps = {
  id: string;
  icon: LucideIcon;
  label: string;
  badge?: string;
  className?: string;
};

function ContainerShell({ id, icon: Icon, label, badge, className }: ContainerShellProps): ReactNode {
  const { selectedNodeId } = useSelection();
  const selected = selectedNodeId === id;
  return (
    <div
      className={cn(
        'size-full overflow-hidden rounded-md border-2 bg-card/40',
        selected && 'ring-2 ring-ring',
        className,
      )}
    >
      <Handle position={Position.Top} type="target" />
      <div
        className="absolute top-0 left-0 flex w-full items-center gap-2 border-b bg-secondary"
        style={{ height: HEADER_ROW, paddingInline: CONTENT_PAD }}
      >
        <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="truncate text-sm font-medium">{label}</span>
        {badge ? (
          <span className="ml-auto rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
            {badge}
          </span>
        ) : null}
      </div>
      <Handle position={Position.Bottom} type="source" />
    </div>
  );
}

export function ConditionNode({ id }: NodeProps<ContainerNodeType<'condition'>>): ReactNode {
  return <ContainerShell id={id} icon={GitBranch} label="Condition" />;
}

export function ParallelNode({ id }: NodeProps<ContainerNodeType<'parallel'>>): ReactNode {
  return <ContainerShell id={id} icon={Split} label="Parallel" />;
}

export function TryNode({ id, data }: NodeProps<ContainerNodeType<'try'>>): ReactNode {
  return (
    <ContainerShell
      id={id}
      icon={ShieldAlert}
      label="Try"
      badge={data.rethrow ? 'rethrow' : undefined}
    />
  );
}

/** React Flow node-type map for the three container kinds. */
export const containerNodeTypes = {
  condition: ConditionNode,
  parallel: ParallelNode,
  try: TryNode,
} satisfies NodeTypes;
