import { Node } from '@pivox/primitives/node';
import { cn } from '@pivox/primitives/utils';
import {
  Handle,
  type Node as FlowNode,
  type NodeProps,
  type NodeTypes,
  Position,
} from '@xyflow/react';
import {
  CircleStop,
  Globe,
  OctagonAlert,
  Variable,
  Workflow,
  type LucideIcon,
} from 'lucide-react';
import type { ReactNode } from 'react';

import type { ActivityNodeData } from '../graph-types';
import { CONTENT_PAD, CONTENT_ROW, HEADER_ROW } from '../grid';
import { useSelection } from '../selection-context';

type ActivityData<K extends ActivityNodeData['kind']> = Extract<
  ActivityNodeData,
  { kind: K }
>;
type ActivityNodeType<K extends ActivityNodeData['kind']> = FlowNode<
  ActivityData<K>,
  K
>;

type ActivityShellProps = {
  id: string;
  icon: LucideIcon;
  label: string;
  /** Omitted (undefined) for the compact `end` node; any string adds the row. */
  summary?: string;
};

// Fixed grid-aligned rows: a HEADER_ROW-tall header, plus a CONTENT_ROW-tall
// summary row when `summary` is present. Total height is a GRID multiple by
// construction and matches the size the transform feeds elk. The shared Node
// primitive hardwires Left/Right handles and `!`-important padding, so its
// chrome is bypassed for full grid control; handles are Top(target)/Bottom(source).
function ActivityShell({
  id,
  icon: Icon,
  label,
  summary,
}: ActivityShellProps): ReactNode {
  const { selectedNodeId } = useSelection();
  const selected = selectedNodeId === id;
  return (
    <Node
      handles={{ target: false, source: false }}
      className={cn(
        'h-full! w-full! gap-0 overflow-hidden p-0',
        selected && 'ring-2 ring-ring',
      )}
    >
      <Handle position={Position.Top} type="target" />
      <div
        className="flex items-center gap-2 border-b bg-secondary"
        style={{ height: HEADER_ROW, paddingInline: CONTENT_PAD }}
      >
        <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="truncate text-sm font-medium">{label}</span>
      </div>
      {summary !== undefined ? (
        <div
          className="flex items-center"
          style={{ height: CONTENT_ROW, paddingInline: CONTENT_PAD }}
        >
          <p className="truncate font-mono text-xs text-muted-foreground">
            {summary}
          </p>
        </div>
      ) : null}
      <Handle position={Position.Bottom} type="source" />
    </Node>
  );
}

const httpSummary = (config: ActivityData<'http'>['config']): string =>
  config.path ? `${config.method} ${config.path}` : config.method;

const setSummary = (config: ActivityData<'set'>['config']): string =>
  Object.keys(config.assignments).join(', ');

export function HttpNode({
  id,
  data,
}: NodeProps<ActivityNodeType<'http'>>): ReactNode {
  return (
    <ActivityShell
      id={id}
      icon={Globe}
      label="HTTP"
      summary={httpSummary(data.config)}
    />
  );
}

export function SetNode({
  id,
  data,
}: NodeProps<ActivityNodeType<'set'>>): ReactNode {
  return (
    <ActivityShell
      id={id}
      icon={Variable}
      label="Set"
      summary={setSummary(data.config)}
    />
  );
}

export function RunWorkflowNode({
  id,
  data,
}: NodeProps<ActivityNodeType<'run_workflow'>>): ReactNode {
  return (
    <ActivityShell
      id={id}
      icon={Workflow}
      label="Run workflow"
      summary={data.config.workflow}
    />
  );
}

export function FailNode({
  id,
  data,
}: NodeProps<ActivityNodeType<'fail'>>): ReactNode {
  return (
    <ActivityShell
      id={id}
      icon={OctagonAlert}
      label="Fail"
      summary={data.config.message}
    />
  );
}

export function EndNode({ id }: NodeProps<ActivityNodeType<'end'>>): ReactNode {
  return <ActivityShell id={id} icon={CircleStop} label="End" />;
}

/** React Flow node-type map for the five activity kinds. */
export const activityNodeTypes = {
  http: HttpNode,
  set: SetNode,
  run_workflow: RunWorkflowNode,
  fail: FailNode,
  end: EndNode,
} satisfies NodeTypes;
