// Canonical presentational data contract for the workflow canvas. `@pivox/features`
// transform imports these types from here (features → ui is the correct direction).
export * from './graph-types';
// The single grid source of truth; the transform imports GRID + row heights.
export * from './grid';

export { WorkflowCanvas, type WorkflowCanvasProps } from './workflow-canvas';
export {
  RunStatus,
  runStatusMeta,
  type RunState,
  type RunStatusMeta,
  type RunStatusProps,
} from './run-status';
export {
  SelectionProvider,
  useSelection,
  type SelectionProviderProps,
} from './selection-context';
export { ConfigPanel } from './config-panel';
export { WorkflowEdgeRenderer, workflowEdgeTypes } from './workflow-edge';
export {
  activityNodeTypes,
  EndNode,
  FailNode,
  HttpNode,
  RunWorkflowNode,
  SetNode,
} from './nodes/activity-node';
export {
  ConditionNode,
  containerNodeTypes,
  ParallelNode,
  TryNode,
} from './nodes/container-node';
export {
  BranchRegionNode,
  ErrorRegionNode,
  LaneRegionNode,
  OtherwiseRegionNode,
  regionNodeTypes,
  TryBodyRegionNode,
  TryCatchRegionNode,
} from './nodes/branch-node';
export { StartNode, startNodeTypes } from './nodes/start-node';
