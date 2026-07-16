import { describe, expect, it } from 'vitest';

import {
  astToGraph,
  type WorkflowNode,
} from '@/workflows/transform/ast-to-graph';
import { formatStepPath, parseStepPath } from '@/workflows/transform/ids';
import type { WorkflowVersion } from '@/workflows/transform/types';

import {
  branch,
  conditionStep,
  countSteps,
  endStep,
  failStep,
  httpStep,
  parallelStep,
  runWorkflowStep,
  seq,
  setStep,
  tryStep,
  version,
} from './ast-fixtures';

const stepNodes = (nodes: WorkflowNode[]): WorkflowNode[] =>
  nodes.filter((n) => n.data.element === 'activity' || n.data.element === 'container');

const regionNodes = (nodes: WorkflowNode[]): WorkflowNode[] =>
  nodes.filter((n) => n.data.element === 'region');

const cases: { name: string; version: WorkflowVersion }[] = [
  { name: 'single activity', version: version(seq(httpStep('a'))) },
  {
    name: 'flat sequence',
    version: version(seq(httpStep('a'), setStep('b'), endStep('c'))),
  },
  {
    name: 'condition with branches and otherwise',
    version: version(
      seq(
        conditionStep(
          'cond',
          [
            branch('x > 1', seq(httpStep('a'))),
            branch('x > 2', seq(setStep('b'), setStep('c'))),
          ],
          seq(endStep('d')),
        ),
      ),
    ),
  },
  {
    name: 'condition without otherwise',
    version: version(
      seq(conditionStep('cond', [branch('ok', seq(httpStep('a')))])),
    ),
  },
  {
    name: 'parallel with N lanes',
    version: version(
      seq(
        parallelStep('par', [
          seq(httpStep('a')),
          seq(setStep('b'), setStep('c')),
          seq(runWorkflowStep('d')),
        ]),
      ),
    ),
  },
  {
    name: 'try with body and catch, rethrow',
    version: version(
      seq(
        tryStep('t', seq(httpStep('a'), failStep('b')), {
          catch: seq(setStep('c')),
          rethrow: true,
        }),
      ),
    ),
  },
  {
    name: 'try body only, no catch',
    version: version(seq(tryStep('t', seq(httpStep('a'))))),
  },
  {
    name: 'deep nesting: try in parallel in condition',
    version: version(
      seq(
        conditionStep('c', [
          branch(
            'guard',
            seq(
              parallelStep('p', [
                seq(tryStep('t', seq(httpStep('deep'), endStep('e')))),
                seq(setStep('lane2')),
              ]),
            ),
          ),
        ]),
      ),
    ),
  },
];

describe('astToGraph', () => {
  it.each(cases)('emits exactly one node per AST step: $name', ({ version: v }) => {
    const { nodes } = astToGraph(v);
    expect(stepNodes(nodes)).toHaveLength(
      countSteps(v.root) + countSteps(v.errorSequence),
    );
  });

  it.each(cases)('assigns unique node ids: $name', ({ version: v }) => {
    const { nodes } = astToGraph(v);
    const ids = nodes.map((n) => n.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it.each(cases)('node id ↔ AST path is a bijection: $name', ({ version: v }) => {
    const { nodes } = astToGraph(v);
    for (const node of stepNodes(nodes)) {
      expect(formatStepPath(parseStepPath(node.id))).toBe(node.id);
      // data.path mirrors the node id.
      const data = node.data;
      if (data.element === 'activity' || data.element === 'container') {
        expect(data.path).toBe(node.id);
      }
    }
  });

  it.each(cases)('every node parentId references an existing node: $name', ({ version: v }) => {
    const { nodes } = astToGraph(v);
    const ids = new Set(nodes.map((n) => n.id));
    for (const node of nodes) {
      if (node.parentId !== undefined) expect(ids.has(node.parentId)).toBe(true);
    }
  });

  it('renders no phantom node for an empty root sequence', () => {
    const { nodes, edges } = astToGraph(version(seq()));
    expect(nodes).toHaveLength(0);
    expect(edges).toHaveLength(0);
  });

  it('links consecutive steps in a flat sequence with sequence edges', () => {
    const { edges } = astToGraph(version(seq(httpStep('a'), setStep('b'), endStep('c'))));
    const seqEdges = edges.filter((e) => e.data?.kind === 'sequence');
    expect(seqEdges.map((e) => [e.source, e.target])).toEqual([
      ['root.steps[0]', 'root.steps[1]'],
      ['root.steps[1]', 'root.steps[2]'],
    ]);
    // The only non-sequence edge is the synthetic start entry.
    expect(edges.filter((e) => e.data?.kind !== 'sequence').map((e) => [e.source, e.target])).toEqual([
      ['start', 'root.steps[0]'],
    ]);
  });

  it.each(cases)('sequence edges never cross a region boundary: $name', ({ version: v }) => {
    const { nodes, edges } = astToGraph(v);
    const parentOf = new Map(nodes.map((n) => [n.id, n.parentId]));
    for (const edge of edges) {
      if (edge.data?.kind !== 'sequence') continue;
      expect(parentOf.get(edge.source)).toBe(parentOf.get(edge.target));
    }
  });

  it('maps each activity kind onto node type and data.kind', () => {
    const { nodes } = astToGraph(
      version(
        seq(
          httpStep('h'),
          setStep('s'),
          runWorkflowStep('r'),
          failStep('f'),
          endStep('e'),
        ),
      ),
    );
    const kinds = nodes
      .filter((n) => n.data.element === 'activity')
      .map((n) => [n.type, n.data.element === 'activity' ? n.data.kind : null]);
    expect(kinds).toEqual([
      ['http', 'http'],
      ['set', 'set'],
      ['run_workflow', 'run_workflow'],
      ['fail', 'fail'],
      ['end', 'end'],
    ]);
  });

  it('carries the activity config and Step.id in node data', () => {
    const { nodes } = astToGraph(version(seq(httpStep('fetch'))));
    const node = nodes.find((n) => n.id === 'root.steps[0]');
    expect(node?.data).toMatchObject({
      element: 'activity',
      kind: 'http',
      stepId: 'fetch',
      path: 'root.steps[0]',
      config: { connector: 'connectors/x', method: 'GET' },
    });
  });

  it('groups condition branches and otherwise under the container', () => {
    const { nodes } = astToGraph(
      version(
        seq(
          conditionStep(
            'cond',
            [branch('a', seq(httpStep('x'))), branch('b', seq(setStep('y')))],
            seq(endStep('z')),
          ),
        ),
      ),
    );
    const container = nodes.find((n) => n.id === 'root.steps[0]');
    expect(container?.type).toBe('condition');
    expect(container?.parentId).toBeUndefined();

    const regions = regionNodes(nodes);
    expect(regions.map((r) => ({ id: r.id, kind: r.data.element === 'region' ? r.data.kind : null, parent: r.parentId }))).toEqual([
      { id: 'root.steps[0].condition.branches[0].then', kind: 'branch', parent: 'root.steps[0]' },
      { id: 'root.steps[0].condition.branches[1].then', kind: 'branch', parent: 'root.steps[0]' },
      { id: 'root.steps[0].condition.otherwise', kind: 'otherwise', parent: 'root.steps[0]' },
    ]);

    // Branch child sits under its branch region.
    const child = nodes.find((n) => n.id === 'root.steps[0].condition.branches[0].then.steps[0]');
    expect(child?.parentId).toBe('root.steps[0].condition.branches[0].then');
  });

  it('labels branch region nodes with the when guard', () => {
    const { nodes } = astToGraph(
      version(seq(conditionStep('c', [branch('user.admin', seq(httpStep('a')))]))),
    );
    const branchRegion = nodes.find(
      (n) => n.id === 'root.steps[0].condition.branches[0].then',
    );
    expect(branchRegion?.data).toMatchObject({ element: 'region', kind: 'branch', when: 'user.admin' });
  });

  it('emits a body region and catch region for a try, carrying rethrow', () => {
    const { nodes } = astToGraph(
      version(seq(tryStep('t', seq(httpStep('a')), { catch: seq(setStep('b')), rethrow: true }))),
    );
    const container = nodes.find((n) => n.id === 'root.steps[0]');
    expect(container?.data).toMatchObject({ element: 'container', kind: 'try', rethrow: true });

    const body = nodes.find((n) => n.id === 'root.steps[0].try.body');
    const cat = nodes.find((n) => n.id === 'root.steps[0].try.catch');
    expect(body?.data).toMatchObject({ element: 'region', kind: 'try-body' });
    expect(cat?.data).toMatchObject({ element: 'region', kind: 'try-catch' });
    expect(body?.parentId).toBe('root.steps[0]');
    expect(cat?.parentId).toBe('root.steps[0]');
  });

  it('omits the catch region when catch is absent', () => {
    const { nodes } = astToGraph(version(seq(tryStep('t', seq(httpStep('a'))))));
    expect(nodes.some((n) => n.id === 'root.steps[0].try.catch')).toBe(false);
    expect(nodes.some((n) => n.id === 'root.steps[0].try.body')).toBe(true);
  });

  it('emits one lane region per parallel branch', () => {
    const { nodes } = astToGraph(
      version(seq(parallelStep('p', [seq(httpStep('a')), seq(setStep('b'))]))),
    );
    const lanes = regionNodes(nodes);
    expect(lanes.map((l) => l.id)).toEqual([
      'root.steps[0].parallel.branches[0]',
      'root.steps[0].parallel.branches[1]',
    ]);
    for (const lane of lanes) expect(lane.parentId).toBe('root.steps[0]');
  });
});

type WorkflowEdges = ReturnType<typeof astToGraph>['edges'];
const edgeTuple = (e: WorkflowEdges[number]) => ({
  source: e.source,
  target: e.target,
  kind: e.data?.kind,
  label: e.label,
});

// Non-sequential edges: everything except the linear `sequence` links and the
// synthetic `start` entry. After the fork/join removal this must always be empty.
const nonSequentialEdges = (edges: WorkflowEdges) =>
  edges.filter((e) => e.data?.kind !== 'sequence' && e.data?.kind !== 'start');

describe('astToGraph sequential wiring', () => {
  it('emits no fork/join/branch/catch edges for a condition — only the container link', () => {
    const graph = astToGraph(
      version(
        seq(
          conditionStep(
            'cond',
            [branch('x > 1', seq(httpStep('a'))), branch('x > 2', seq(setStep('b')))],
            seq(endStep('c')),
          ),
          endStep('after'),
        ),
      ),
    );
    // No boundary edges into the regions at all.
    expect(nonSequentialEdges(graph.edges)).toEqual([]);
    // The container is wired to the following step like any other sequence step.
    const seqEdges = graph.edges.filter((e) => e.data?.kind === 'sequence').map(edgeTuple);
    expect(seqEdges).toContainEqual({
      source: 'root.steps[0]',
      target: 'root.steps[1]',
      kind: 'sequence',
      label: undefined,
    });
  });

  it('drops fork/join edges and the join marker node for a parallel', () => {
    const graph = astToGraph(
      version(
        seq(
          parallelStep('p', [seq(httpStep('a')), seq(setStep('b'), setStep('c'))]),
          endStep('after'),
        ),
      ),
    );
    // Only intra-lane sequence links remain, plus the container→after link.
    expect(nonSequentialEdges(graph.edges)).toEqual([]);
    expect(graph.edges.filter((e) => e.data?.kind === 'sequence').map(edgeTuple)).toEqual([
      {
        source: 'root.steps[0].parallel.branches[1].steps[0]',
        target: 'root.steps[0].parallel.branches[1].steps[1]',
        kind: 'sequence',
        label: undefined,
      },
      { source: 'root.steps[0]', target: 'root.steps[1]', kind: 'sequence', label: undefined },
    ]);
  });

  it('emits no join marker node even when the parallel is last', () => {
    const graph = astToGraph(
      version(seq(parallelStep('p', [seq(httpStep('a')), seq(setStep('b'))]))),
    );
    expect(graph.nodes.some((n) => n.id === 'root.steps[0].join')).toBe(false);
    expect(graph.edges).toEqual([
      expect.objectContaining({ source: 'start', target: 'root.steps[0]' }),
    ]);
  });

  it('emits no body/catch entry edges for a try', () => {
    const graph = astToGraph(
      version(seq(tryStep('t', seq(httpStep('a')), { catch: seq(setStep('b')) }))),
    );
    expect(nonSequentialEdges(graph.edges)).toEqual([]);
  });

  it('uses only start + sequence edge kinds even under deep nesting', () => {
    const graph = astToGraph(
      version(
        seq(
          conditionStep('c', [
            branch(
              'guard',
              seq(
                parallelStep('p', [
                  seq(tryStep('t', seq(httpStep('deep'), endStep('e')))),
                  seq(setStep('lane2')),
                ]),
              ),
            ),
          ]),
        ),
      ),
    );
    expect(new Set(graph.edges.map((e) => e.data?.kind))).toEqual(new Set(['start', 'sequence']));
    // Every edge references two real nodes (regions are not edge endpoints).
    const ids = new Set(graph.nodes.map((n) => n.id));
    for (const e of graph.edges) {
      expect(ids.has(e.source)).toBe(true);
      expect(ids.has(e.target)).toBe(true);
    }
  });

  it('assigns unique edge ids', () => {
    const graph = astToGraph(
      version(seq(parallelStep('p', [seq(httpStep('a'))]), endStep('after'))),
    );
    const ids = graph.edges.map((e) => e.id);
    expect(new Set(ids).size).toBe(ids.length);
  });
});

describe('astToGraph error_sequence', () => {
  it('renders error_sequence as a separate "on error" region with its own id space', () => {
    const graph = astToGraph(
      version(seq(httpStep('main')), seq(setStep('cleanup'), failStep('rethrow'))),
    );
    const errorRegion = graph.nodes.find((n) => n.id === 'error');
    expect(errorRegion?.data).toMatchObject({ element: 'region', kind: 'error' });
    expect(errorRegion?.parentId).toBeUndefined();

    const cleanup = graph.nodes.find((n) => n.id === 'error.steps[0]');
    const rethrow = graph.nodes.find((n) => n.id === 'error.steps[1]');
    expect(cleanup?.parentId).toBe('error');
    expect(rethrow?.parentId).toBe('error');
    expect(cleanup?.data).toMatchObject({ element: 'activity', kind: 'set', path: 'error.steps[0]' });

    // The error region is not wired into the main flow.
    const crossing = graph.edges.filter(
      (e) => e.source.startsWith('error') !== e.target.startsWith('error'),
    );
    expect(crossing).toHaveLength(0);
  });

  it('renders no error region when error_sequence is absent', () => {
    const graph = astToGraph(version(seq(httpStep('main'))));
    expect(graph.nodes.some((n) => n.id === 'error')).toBe(false);
    expect(graph.nodes.some((n) => n.id.startsWith('error'))).toBe(false);
  });
});

describe('astToGraph start node', () => {
  it('prepends a synthetic start node with an edge into root\'s first step', () => {
    const graph = astToGraph(version(seq(httpStep('a'), setStep('b'))));

    const start = graph.nodes.find((n) => n.id === 'start');
    expect(start?.data).toEqual({ element: 'start' });
    expect(start?.type).toBe('start');
    expect(start?.parentId).toBeUndefined();

    const startEdges = graph.edges.filter((e) => e.data?.kind === 'start');
    expect(startEdges.map((e) => [e.source, e.target])).toEqual([
      ['start', 'root.steps[0]'],
    ]);
  });

  it('points the start edge at the container when root begins with one', () => {
    const graph = astToGraph(
      version(seq(conditionStep('c', [branch('ok', seq(httpStep('a')))]))),
    );
    const startEdge = graph.edges.find((e) => e.data?.kind === 'start');
    expect(startEdge?.target).toBe('root.steps[0]');
  });

  it('omits the start node for an empty root sequence', () => {
    const graph = astToGraph(version(seq()));
    expect(graph.nodes.some((n) => n.id === 'start')).toBe(false);
    expect(graph.edges.some((e) => e.data?.kind === 'start')).toBe(false);
  });

  it('emits exactly one start node regardless of root length', () => {
    const graph = astToGraph(version(seq(httpStep('a'), setStep('b'), endStep('c'))));
    expect(graph.nodes.filter((n) => n.data.element === 'start')).toHaveLength(1);
  });
});
