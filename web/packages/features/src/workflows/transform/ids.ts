// Stable node ids ↔ AST step paths.
//
// A node id encodes the location of an AST element by mirroring proto field
// access, e.g. `root.steps[2].try.body.steps[0]`. Two id spaces share the
// grammar:
//
//   step id    — always ends in `.steps[i]`; identifies one Step. Carried on
//                every step node and used as the run-state / edit join key.
//   region id  — ends in a container-region suffix (`.try.body`, `.try.catch`,
//                `.condition.branches[k].then`, `.condition.otherwise`,
//                `.parallel.branches[k]`); identifies a nested Sequence and is
//                the React Flow group node wrapping that region's steps. A
//                region id doubles as the id prefix for its child steps.
//
// The main sequence has no group node; its prefix is `ROOT`. The version's
// `error_sequence` is a distinct id space prefixed `ERROR_ROOT`.
//
// `START` is a synthetic entry node with no AST counterpart; it is excluded from
// step-path parsing (like a Parallel `join` marker) and never carries frames.

export const ROOT = 'root';
export const ERROR_ROOT = 'error';
export const START = 'start';

export type RootKind = typeof ROOT | typeof ERROR_ROOT;

/** Descent into a container step's nested sequence. */
export type Region =
  | { region: 'tryBody' }
  | { region: 'tryCatch' }
  | { region: 'conditionBranch'; branch: number }
  | { region: 'conditionOtherwise' }
  | { region: 'parallelBranch'; branch: number };

/**
 * One hop of a step path: select `steps[index]` in the current sequence, then
 * optionally descend `into` a nested region. Only the final frame of a path
 * omits `into` (it targets the step itself).
 */
export type PathFrame = { index: number; into?: Region };

/** A located step: which top-level sequence (`root`/`error`) plus its frames. */
export type StepPath = { root: RootKind; frames: PathFrame[] };

function regionSuffix(region: Region): string {
  switch (region.region) {
    case 'tryBody':
      return '.try.body';
    case 'tryCatch':
      return '.try.catch';
    case 'conditionBranch':
      return `.condition.branches[${region.branch}].then`;
    case 'conditionOtherwise':
      return '.condition.otherwise';
    case 'parallelBranch':
      return `.parallel.branches[${region.branch}]`;
    default: {
      const exhaustive: never = region;
      return exhaustive;
    }
  }
}

const STEP_RE = /^\.steps\[(\d+)\]/;
const REGION_RES: { re: RegExp; region: (m: RegExpExecArray) => Region }[] = [
  { re: /^\.try\.body/, region: () => ({ region: 'tryBody' }) },
  { re: /^\.try\.catch/, region: () => ({ region: 'tryCatch' }) },
  {
    re: /^\.condition\.branches\[(\d+)\]\.then/,
    region: (m) => ({ region: 'conditionBranch', branch: Number(m[1]) }),
  },
  { re: /^\.condition\.otherwise/, region: () => ({ region: 'conditionOtherwise' }) },
  {
    re: /^\.parallel\.branches\[(\d+)\]/,
    region: (m) => ({ region: 'parallelBranch', branch: Number(m[1]) }),
  },
];

/** Serializes a structured step path to its node id. */
export function formatStepPath(path: StepPath): string {
  let id: string = path.root;
  for (const frame of path.frames) {
    id += `.steps[${frame.index}]`;
    if (frame.into) id += regionSuffix(frame.into);
  }
  return id;
}

/** Parses a step-node id back to its structured path. Inverse of formatStepPath. */
export function parseStepPath(id: string): StepPath {
  let root: RootKind;
  if (id === ROOT || id.startsWith(`${ROOT}.`)) {
    root = ROOT;
  } else if (id === ERROR_ROOT || id.startsWith(`${ERROR_ROOT}.`)) {
    root = ERROR_ROOT;
  } else {
    throw new Error(`invalid step path: ${id}`);
  }

  let rest = id.slice(root.length);
  const frames: PathFrame[] = [];
  while (rest.length > 0) {
    const stepMatch = STEP_RE.exec(rest);
    if (!stepMatch) throw new Error(`invalid step path segment: ${rest}`);
    const index = Number(stepMatch[1]);
    rest = rest.slice(stepMatch[0].length);

    let into: Region | undefined;
    for (const { re, region } of REGION_RES) {
      const m = re.exec(rest);
      if (m) {
        into = region(m);
        rest = rest.slice(m[0].length);
        break;
      }
    }
    frames.push(into ? { index, into } : { index });
  }
  return { root, frames };
}

/** The node id of the `index`th step within the sequence at `regionId`. */
export function childStepId(regionId: string, index: number): string {
  return `${regionId}.steps[${index}]`;
}

export function tryBodyRegionId(stepId: string): string {
  return `${stepId}.try.body`;
}

export function tryCatchRegionId(stepId: string): string {
  return `${stepId}.try.catch`;
}

export function conditionBranchRegionId(stepId: string, branch: number): string {
  return `${stepId}.condition.branches[${branch}].then`;
}

export function conditionOtherwiseRegionId(stepId: string): string {
  return `${stepId}.condition.otherwise`;
}

export function parallelBranchRegionId(stepId: string, branch: number): string {
  return `${stepId}.parallel.branches[${branch}]`;
}
