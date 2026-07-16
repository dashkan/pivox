import {
  ACTIVITY_NODE_HEIGHT,
  ACTIVITY_NODE_HEIGHT_COMPACT,
  NODE_WIDTH,
  START_HEIGHT,
  START_WIDTH,
} from '@pivox/ui/workflow';
import type {
  ActivityConfig,
  EdgeKind,
  WorkflowEdge,
  WorkflowGraph,
  WorkflowNode,
} from '@pivox/ui/workflow';

import {
  childStepId,
  conditionBranchRegionId,
  conditionOtherwiseRegionId,
  ERROR_ROOT,
  parallelBranchRegionId,
  ROOT,
  START,
  tryBodyRegionId,
  tryCatchRegionId,
} from './ids';
import type { Sequence, Step, WorkflowVersion } from './types';

export type {
  ActivityConfig,
  ActivityKind,
  ActivityNodeData,
  ContainerKind,
  ContainerNodeData,
  EdgeKind,
  RegionKind,
  RegionNodeData,
  StartNodeData,
  WorkflowEdge,
  WorkflowEdgeData,
  WorkflowGraph,
  WorkflowNode,
  WorkflowNodeData,
} from '@pivox/ui/workflow';

function activityConfig(activity: NonNullable<Step['activity']>): ActivityConfig {
  if (activity.http) return { kind: 'http', config: activity.http };
  if (activity.set) return { kind: 'set', config: activity.set };
  if (activity.runWorkflow) return { kind: 'run_workflow', config: activity.runWorkflow };
  if (activity.fail) return { kind: 'fail', config: activity.fail };
  if (activity.end) return { kind: 'end', config: activity.end };
  throw new Error('activity has no kind set');
}

const sequenceSteps = (sequence: Sequence | undefined): Step[] => sequence?.steps ?? [];

/**
 * Walks a `WorkflowVersion` (`root` and, if present, `error_sequence`) into
 * React Flow nodes + edges.
 *
 * One node per Step: leaf steps become activity nodes; Condition/Parallel/Try
 * become container nodes wrapping region group nodes (branches, lanes,
 * body/catch) that parent their child steps via `parentId`. A synthetic `start`
 * node is prepended with an edge into `root`'s first step (omitted for an empty
 * root). `error_sequence` renders as its own top-level "on error" region, keyed
 * under the `error` id space and never wired into the main flow.
 *
 * Edges are strictly sequential: consecutive steps within one Sequence get a
 * linear `sequence` edge, and a container is itself a step in its parent
 * sequence, so it connects top/bottom like any leaf. There are NO fork/join or
 * branch-entry edges — the regions are visually contained (laid out LTR by
 * `layout.ts`) but not edge-wired to their container.
 */
export function astToGraph(version: WorkflowVersion): WorkflowGraph {
  const nodes: WorkflowNode[] = [];
  const edges: WorkflowEdge[] = [];

  const addEdge = (source: string, target: string, kind: EdgeKind): void => {
    edges.push({
      id: `${kind}:${source}=>${target}`,
      source,
      target,
      data: { kind },
    });
  };

  const walkSequence = (
    sequence: Sequence | undefined,
    regionId: string,
    parentId: string | undefined,
  ): void => {
    const steps = sequenceSteps(sequence);
    let previousId: string | undefined;

    steps.forEach((step, index) => {
      const stepId = childStepId(regionId, index);
      walkStep(step, stepId, parentId);

      if (previousId !== undefined) addEdge(previousId, stepId, 'sequence');
      previousId = stepId;
    });
  };

  const walkStep = (
    step: Step,
    stepId: string,
    parentId: string | undefined,
  ): void => {
    const base = { position: { x: 0, y: 0 }, parentId } satisfies Partial<WorkflowNode>;

    if (step.activity) {
      const activity = activityConfig(step.activity);
      // `end` renders header-only (no summary row); every other kind adds a
      // summary row. The height MUST match the node component's rendered rows.
      const height =
        activity.kind === 'end' ? ACTIVITY_NODE_HEIGHT_COMPACT : ACTIVITY_NODE_HEIGHT;
      nodes.push({
        ...base,
        id: stepId,
        type: activity.kind,
        width: NODE_WIDTH,
        height,
        data: {
          element: 'activity',
          stepId: step.id,
          path: stepId,
          ...activity,
        },
      });
      return;
    }

    if (step.condition) {
      nodes.push({
        ...base,
        id: stepId,
        type: 'condition',
        data: { element: 'container', kind: 'condition', stepId: step.id, path: stepId },
      });
      step.condition.branches.forEach((branch, k) => {
        const branchRegion = conditionBranchRegionId(stepId, k);
        nodes.push({
          id: branchRegion,
          type: 'branch',
          position: { x: 0, y: 0 },
          parentId: stepId,
          data: { element: 'region', kind: 'branch', when: branch.when },
        });
        walkSequence(branch.then, branchRegion, branchRegion);
      });
      if (step.condition.otherwise) {
        const otherwiseRegion = conditionOtherwiseRegionId(stepId);
        nodes.push({
          id: otherwiseRegion,
          type: 'otherwise',
          position: { x: 0, y: 0 },
          parentId: stepId,
          data: { element: 'region', kind: 'otherwise' },
        });
        walkSequence(step.condition.otherwise, otherwiseRegion, otherwiseRegion);
      }
      return;
    }

    if (step.parallel) {
      nodes.push({
        ...base,
        id: stepId,
        type: 'parallel',
        data: { element: 'container', kind: 'parallel', stepId: step.id, path: stepId },
      });
      step.parallel.branches.forEach((lane, k) => {
        const laneRegion = parallelBranchRegionId(stepId, k);
        nodes.push({
          id: laneRegion,
          type: 'lane',
          position: { x: 0, y: 0 },
          parentId: stepId,
          data: { element: 'region', kind: 'lane' },
        });
        walkSequence(lane, laneRegion, laneRegion);
      });
      return;
    }

    if (step.try) {
      nodes.push({
        ...base,
        id: stepId,
        type: 'try',
        data: {
          element: 'container',
          kind: 'try',
          stepId: step.id,
          path: stepId,
          rethrow: step.try.rethrow ?? false,
        },
      });
      const bodyRegion = tryBodyRegionId(stepId);
      nodes.push({
        id: bodyRegion,
        type: 'try-body',
        position: { x: 0, y: 0 },
        parentId: stepId,
        data: { element: 'region', kind: 'try-body' },
      });
      walkSequence(step.try.body, bodyRegion, bodyRegion);
      if (step.try.catch) {
        const catchRegion = tryCatchRegionId(stepId);
        nodes.push({
          id: catchRegion,
          type: 'try-catch',
          position: { x: 0, y: 0 },
          parentId: stepId,
          data: { element: 'region', kind: 'try-catch' },
        });
        walkSequence(step.try.catch, catchRegion, catchRegion);
      }
      return;
    }

    throw new Error(`step ${step.id} has no kind set`);
  };

  const rootSteps = sequenceSteps(version.root);
  if (rootSteps.length > 0) {
    nodes.push({
      id: START,
      type: 'start',
      position: { x: 0, y: 0 },
      width: START_WIDTH,
      height: START_HEIGHT,
      data: { element: 'start' },
    });
    addEdge(START, childStepId(ROOT, 0), 'start');
  }

  walkSequence(version.root, ROOT, undefined);

  if (sequenceSteps(version.errorSequence).length > 0) {
    nodes.push({
      id: ERROR_ROOT,
      type: 'error',
      position: { x: 0, y: 0 },
      data: { element: 'region', kind: 'error' },
    });
    walkSequence(version.errorSequence, ERROR_ROOT, ERROR_ROOT);
  }

  return { nodes, edges };
}
