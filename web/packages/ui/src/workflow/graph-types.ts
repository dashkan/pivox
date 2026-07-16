import type { components } from '@pivox/client';
import type { Edge, Node } from '@xyflow/react';

// Presentational data contract for the workflow canvas. `@pivox/features`
// transform (`ast-to-graph`) produces these; the `@pivox/ui/workflow`
// renderers consume them. Lives here because `features` depends on `ui`, so
// `ui` cannot import from `features` — this is the canonical shared contract.

type Schemas = components['schemas'];

export type HttpActivity = Schemas['v1HttpActivity'];
export type SetActivity = Schemas['v1SetActivity'];
export type RunWorkflowActivity = Schemas['v1RunWorkflowActivity'];
export type FailActivity = Schemas['v1FailActivity'];
export type EndActivity = Schemas['v1EndActivity'];

export type ActivityKind = 'http' | 'set' | 'run_workflow' | 'fail' | 'end';
export type ContainerKind = 'condition' | 'parallel' | 'try';
export type RegionKind = 'branch' | 'otherwise' | 'lane' | 'try-body' | 'try-catch' | 'error';
// Only two edge kinds remain: the synthetic entry edge and the linear step link.
// Container regions are visually contained, never edge-wired to a fork/join.
export type EdgeKind = 'start' | 'sequence';

export type ActivityConfig =
  | { kind: 'http'; config: HttpActivity }
  | { kind: 'set'; config: SetActivity }
  | { kind: 'run_workflow'; config: RunWorkflowActivity }
  | { kind: 'fail'; config: FailActivity }
  | { kind: 'end'; config: EndActivity };

export type ActivityNodeData = ActivityConfig & {
  element: 'activity';
  /** The AST Step.id (unique within the version; run-state join key). */
  stepId: string;
  /** The structural AST path; equal to the node id. */
  path: string;
};

export type ContainerNodeData = {
  element: 'container';
  kind: ContainerKind;
  stepId: string;
  path: string;
  /** Try only: whether the original error is re-raised after `catch`. */
  rethrow?: boolean;
};

export type RegionNodeData = {
  element: 'region';
  kind: RegionKind;
  /** Condition branch only: the `when` CEL guard. */
  when?: string;
};

/** Synthetic entry node; sits above `root`'s first step. No AST counterpart. */
export type StartNodeData = { element: 'start' };

export type WorkflowNodeData =
  | ActivityNodeData
  | ContainerNodeData
  | RegionNodeData
  | StartNodeData;

export type WorkflowEdgeData = { kind: EdgeKind };

export type WorkflowNode = Node<WorkflowNodeData>;
export type WorkflowEdge = Edge<WorkflowEdgeData>;

export type WorkflowGraph = {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
};
