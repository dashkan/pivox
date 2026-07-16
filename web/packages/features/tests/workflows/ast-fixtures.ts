import type {
  Branch,
  Sequence,
  Step,
  WorkflowVersion,
} from '@/workflows/transform/types';

export const seq = (...steps: Step[]): Sequence => ({ steps });

export const version = (root: Sequence, errorSequence?: Sequence): WorkflowVersion => ({
  root,
  ...(errorSequence ? { errorSequence } : {}),
});

export const httpStep = (id: string): Step => ({
  id,
  activity: { http: { connector: 'connectors/x', method: 'GET', path: '/p' } },
});

export const setStep = (id: string): Step => ({
  id,
  activity: { set: { assignments: { total: 'a + b' } } },
});

export const runWorkflowStep = (id: string): Step => ({
  id,
  activity: { runWorkflow: { workflow: 'workflows/sub' } },
});

export const failStep = (id: string): Step => ({
  id,
  activity: { fail: { message: 'boom' } },
});

export const endStep = (id: string): Step => ({ id, activity: { end: {} } });

export const branch = (when: string, then: Sequence): Branch => ({ when, then });

export const conditionStep = (
  id: string,
  branches: Branch[],
  otherwise?: Sequence,
): Step => ({ id, condition: { branches, otherwise } });

export const parallelStep = (id: string, branches: Sequence[]): Step => ({
  id,
  parallel: { branches },
});

export const tryStep = (
  id: string,
  body: Sequence,
  options: { catch?: Sequence; rethrow?: boolean } = {},
): Step => ({
  id,
  try: { body, catch: options.catch, rethrow: options.rethrow },
});

/** Recursively counts every Step object reachable from a sequence. */
export function countSteps(sequence: Sequence | undefined): number {
  const steps = sequence?.steps ?? [];
  return steps.reduce((total, step) => {
    let nested = 0;
    if (step.condition) {
      nested += step.condition.branches.reduce((n, b) => n + countSteps(b.then), 0);
      nested += countSteps(step.condition.otherwise);
    }
    if (step.parallel) {
      nested += step.parallel.branches.reduce((n, lane) => n + countSteps(lane), 0);
    }
    if (step.try) {
      nested += countSteps(step.try.body);
      nested += countSteps(step.try.catch);
    }
    return total + 1 + nested;
  }, 0);
}
