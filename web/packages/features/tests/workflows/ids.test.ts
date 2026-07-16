import { describe, expect, it } from 'vitest';

import {
  childStepId,
  conditionBranchRegionId,
  conditionOtherwiseRegionId,
  ERROR_ROOT,
  formatStepPath,
  parallelBranchRegionId,
  parseStepPath,
  ROOT,
  tryBodyRegionId,
  tryCatchRegionId,
  type StepPath,
} from '@/workflows/transform/ids';

describe('formatStepPath', () => {
  const cases: { name: string; path: StepPath; id: string }[] = [
    { name: 'top-level step', path: { root: 'root', frames: [{ index: 0 }] }, id: 'root.steps[0]' },
    { name: 'later top-level step', path: { root: 'root', frames: [{ index: 3 }] }, id: 'root.steps[3]' },
    {
      name: 'try body',
      path: { root: 'root', frames: [{ index: 2, into: { region: 'tryBody' } }, { index: 0 }] },
      id: 'root.steps[2].try.body.steps[0]',
    },
    {
      name: 'try catch',
      path: { root: 'root', frames: [{ index: 1, into: { region: 'tryCatch' } }, { index: 4 }] },
      id: 'root.steps[1].try.catch.steps[4]',
    },
    {
      name: 'condition branch',
      path: {
        root: 'root',
        frames: [
          { index: 0, into: { region: 'conditionBranch', branch: 2 } },
          { index: 1 },
        ],
      },
      id: 'root.steps[0].condition.branches[2].then.steps[1]',
    },
    {
      name: 'condition otherwise',
      path: {
        root: 'root',
        frames: [{ index: 0, into: { region: 'conditionOtherwise' } }, { index: 0 }],
      },
      id: 'root.steps[0].condition.otherwise.steps[0]',
    },
    {
      name: 'parallel lane',
      path: {
        root: 'root',
        frames: [
          { index: 5, into: { region: 'parallelBranch', branch: 1 } },
          { index: 0 },
        ],
      },
      id: 'root.steps[5].parallel.branches[1].steps[0]',
    },
    {
      name: 'try in parallel in condition',
      path: {
        root: 'root',
        frames: [
          { index: 1, into: { region: 'conditionBranch', branch: 0 } },
          { index: 2, into: { region: 'parallelBranch', branch: 3 } },
          { index: 0, into: { region: 'tryBody' } },
          { index: 4 },
        ],
      },
      id: 'root.steps[1].condition.branches[0].then.steps[2].parallel.branches[3].steps[0].try.body.steps[4]',
    },
    {
      name: 'error-sequence step',
      path: { root: 'error', frames: [{ index: 1 }] },
      id: 'error.steps[1]',
    },
    {
      name: 'nested error-sequence step',
      path: { root: 'error', frames: [{ index: 0, into: { region: 'tryBody' } }, { index: 0 }] },
      id: 'error.steps[0].try.body.steps[0]',
    },
  ];

  it.each(cases)('formats $name', ({ path, id }) => {
    expect(formatStepPath(path)).toBe(id);
  });

  it.each(cases)('parses $name', ({ path, id }) => {
    expect(parseStepPath(id)).toEqual(path);
  });

  it.each(cases)('round-trips $name through parse ∘ format', ({ path }) => {
    expect(parseStepPath(formatStepPath(path))).toEqual(path);
  });

  it.each(cases)('round-trips $name through format ∘ parse', ({ id }) => {
    expect(formatStepPath(parseStepPath(id))).toBe(id);
  });

  it('rejects an id not rooted at root or error', () => {
    expect(() => parseStepPath('steps[0]')).toThrow();
    expect(() => parseStepPath('nonsense.steps[0]')).toThrow();
  });

  it('rejects a malformed segment', () => {
    expect(() => parseStepPath('root.frobnicate')).toThrow();
  });
});

describe('region id builders', () => {
  it('child step id nests under a region prefix', () => {
    expect(childStepId(ROOT, 2)).toBe('root.steps[2]');
    expect(childStepId(ERROR_ROOT, 0)).toBe('error.steps[0]');
    expect(childStepId('root.steps[0].try.body', 1)).toBe(
      'root.steps[0].try.body.steps[1]',
    );
  });

  it('builds region ids that are parseable as their child prefix', () => {
    const step = 'root.steps[0]';
    expect(tryBodyRegionId(step)).toBe('root.steps[0].try.body');
    expect(tryCatchRegionId(step)).toBe('root.steps[0].try.catch');
    expect(conditionBranchRegionId(step, 1)).toBe(
      'root.steps[0].condition.branches[1].then',
    );
    expect(conditionOtherwiseRegionId(step)).toBe('root.steps[0].condition.otherwise');
    expect(parallelBranchRegionId(step, 2)).toBe('root.steps[0].parallel.branches[2]');
  });
});
