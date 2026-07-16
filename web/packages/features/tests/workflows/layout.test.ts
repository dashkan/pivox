import { describe, expect, it } from 'vitest';

import {
  ACTIVITY_NODE_HEIGHT,
  ACTIVITY_NODE_HEIGHT_COMPACT,
  NODE_WIDTH,
  START_HEIGHT,
  START_WIDTH,
} from '@pivox/ui/workflow';

import { astToGraph } from '@/workflows/transform/ast-to-graph';
import { GRID, layoutGraph } from '@/workflows/transform/layout';

import {
  branch,
  conditionStep,
  endStep,
  httpStep,
  parallelStep,
  seq,
  setStep,
  tryStep,
  version,
} from './ast-fixtures';

const deep = version(
  seq(
    conditionStep(
      'c',
      [
        branch(
          'guard',
          seq(
            parallelStep('p', [
              seq(tryStep('t', seq(httpStep('deep'), endStep('e')), { catch: seq(setStep('c')) })),
              seq(setStep('lane2')),
            ]),
          ),
        ),
      ],
      seq(httpStep('else')),
    ),
    setStep('after'),
  ),
  seq(setStep('cleanup')),
);

describe('layoutGraph', () => {
  it('lays out a deep graph whose only edges are start + sequence', async () => {
    const graph = astToGraph(deep);
    // Sanity: post fork/join removal the deep graph carries only start/sequence.
    expect(new Set(graph.edges.map((e) => e.data?.kind))).toEqual(
      new Set(['start', 'sequence']),
    );
    const laid = await layoutGraph(graph);
    expect(laid.nodes).toHaveLength(graph.nodes.length);
  });

  it('preserves node identity, count, and parent grouping', async () => {
    const graph = astToGraph(deep);
    const laid = await layoutGraph(graph);

    expect(laid.nodes.map((n) => n.id).sort()).toEqual(
      graph.nodes.map((n) => n.id).sort(),
    );
    expect(laid.edges).toEqual(graph.edges);

    const parentBefore = new Map(graph.nodes.map((n) => [n.id, n.parentId]));
    for (const node of laid.nodes) {
      expect(node.parentId).toBe(parentBefore.get(node.id));
    }
  });

  it('assigns a finite position to every node', async () => {
    const laid = await layoutGraph(astToGraph(deep));
    for (const node of laid.nodes) {
      expect(Number.isFinite(node.position.x)).toBe(true);
      expect(Number.isFinite(node.position.y)).toBe(true);
    }
  });

  it('sizes container and region nodes from their children', async () => {
    const laid = await layoutGraph(astToGraph(deep));
    const containersAndRegions = laid.nodes.filter(
      (n) => n.data.element === 'container' || n.data.element === 'region',
    );
    expect(containersAndRegions.length).toBeGreaterThan(0);
    for (const node of containersAndRegions) {
      expect(node.width ?? 0).toBeGreaterThan(0);
      expect(node.height ?? 0).toBeGreaterThan(0);
    }
  });

  it('keeps every child inside its parent with a visible margin on all sides', async () => {
    const laid = await layoutGraph(astToGraph(deep));
    const byId = new Map(laid.nodes.map((n) => [n.id, n]));
    // The box must not merely contain the child — it must clear it (and the
    // node's handles) on every side. Guards against the reported bottom clipping.
    const MARGIN = 16;
    for (const node of laid.nodes) {
      if (node.parentId === undefined) continue;
      const parent = byId.get(node.parentId)!;
      expect(node.position.x).toBeGreaterThanOrEqual(MARGIN);
      expect(node.position.y).toBeGreaterThanOrEqual(MARGIN);
      expect(node.position.x + (node.width ?? 0)).toBeLessThanOrEqual((parent.width ?? 0) - MARGIN);
      expect(node.position.y + (node.height ?? 0)).toBeLessThanOrEqual((parent.height ?? 0) - MARGIN);
    }
  });

  it('handles an empty graph', async () => {
    const laid = await layoutGraph({ nodes: [], edges: [] });
    expect(laid.nodes).toEqual([]);
    expect(laid.edges).toEqual([]);
  });
});

describe('layoutGraph arrangement', () => {
  const byId = (nodes: Awaited<ReturnType<typeof layoutGraph>>['nodes']) =>
    new Map(nodes.map((n) => [n.id, n]));

  it('arranges condition regions left-to-right with otherwise rightmost', async () => {
    const v = version(
      seq(
        conditionStep(
          'cond',
          [
            branch('x > 1', seq(httpStep('a'), setStep('a2'))),
            branch('x > 2', seq(setStep('b'))),
          ],
          seq(endStep('d')),
        ),
      ),
    );
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    const b0 = nodes.get('root.steps[0].condition.branches[0].then')!;
    const b1 = nodes.get('root.steps[0].condition.branches[1].then')!;
    const other = nodes.get('root.steps[0].condition.otherwise')!;
    // Region positions are relative to the shared container, so x-order is direct.
    expect(b0.position.x).toBeLessThan(b1.position.x);
    expect(b1.position.x).toBeLessThan(other.position.x);
  });

  it('stacks steps vertically within a region', async () => {
    const v = version(
      seq(conditionStep('cond', [branch('ok', seq(httpStep('a'), setStep('b'), endStep('c')))])),
    );
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    const region = 'root.steps[0].condition.branches[0].then';
    const s0 = nodes.get(`${region}.steps[0]`)!;
    const s1 = nodes.get(`${region}.steps[1]`)!;
    const s2 = nodes.get(`${region}.steps[2]`)!;
    expect(s0.position.y).toBeLessThan(s1.position.y);
    expect(s1.position.y).toBeLessThan(s2.position.y);
    // Same column: steps share an x within the region.
    expect(s0.position.x).toBe(s1.position.x);
    expect(s1.position.x).toBe(s2.position.x);
  });

  it('arranges parallel lanes left-to-right', async () => {
    const v = version(seq(parallelStep('p', [seq(httpStep('a')), seq(setStep('b')), seq(endStep('c'))])));
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    const l0 = nodes.get('root.steps[0].parallel.branches[0]')!;
    const l1 = nodes.get('root.steps[0].parallel.branches[1]')!;
    const l2 = nodes.get('root.steps[0].parallel.branches[2]')!;
    expect(l0.position.x).toBeLessThan(l1.position.x);
    expect(l1.position.x).toBeLessThan(l2.position.x);
  });

  it('places try body left of catch', async () => {
    const v = version(seq(tryStep('t', seq(httpStep('a')), { catch: seq(setStep('b')) })));
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    const body = nodes.get('root.steps[0].try.body')!;
    const cat = nodes.get('root.steps[0].try.catch')!;
    expect(body.position.x).toBeLessThan(cat.position.x);
  });

  it('positions the start node above root\'s first step', async () => {
    const v = version(seq(httpStep('a'), setStep('b')));
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    const start = nodes.get('start')!;
    const first = nodes.get('root.steps[0]')!;
    expect(start.position.y).toBeLessThan(first.position.y);
  });

  it('places the on-error region to the right of the main flow, without overlap', async () => {
    const v = version(seq(httpStep('a'), setStep('b')), seq(setStep('cleanup')));
    const laid = await layoutGraph(astToGraph(v));
    const nodes = byId(laid.nodes);
    const error = nodes.get('error')!;
    // Right of every top-level main-flow node (start + root steps).
    const mainRight = laid.nodes
      .filter((n) => n.parentId === undefined && n.id !== 'error')
      .reduce((max, n) => Math.max(max, n.position.x + (n.width ?? 0)), 0);
    expect(error.position.x).toBeGreaterThanOrEqual(mainRight);
  });

  it('sizes the on-error region to fully contain its last child (no clipping)', async () => {
    const v = version(seq(httpStep('a')), seq(setStep('c0'), httpStep('c1'), setStep('c2')));
    const laid = await layoutGraph(astToGraph(v));
    const nodes = byId(laid.nodes);
    const error = nodes.get('error')!;
    const last = nodes.get('error.steps[2]')!;
    // Child position is relative to the region; its bottom must sit inside with
    // a positive bottom gap (breathing room, not flush against the box edge).
    const bottomGap = (error.height ?? 0) - (last.position.y + (last.height ?? 0));
    expect(bottomGap).toBeGreaterThan(0);
  });
});

describe('layoutGraph grid alignment', () => {
  const byId = (nodes: Awaited<ReturnType<typeof layoutGraph>>['nodes']) =>
    new Map(nodes.map((n) => [n.id, n]));

  it('snaps every node position AND size to a GRID multiple (nested included)', async () => {
    const laid = await layoutGraph(astToGraph(deep));
    expect(laid.nodes.length).toBeGreaterThan(0);
    for (const node of laid.nodes) {
      expect(node.position.x % GRID).toBe(0);
      expect(node.position.y % GRID).toBe(0);
      expect((node.width ?? 0) % GRID).toBe(0);
      expect((node.height ?? 0) % GRID).toBe(0);
    }
  });

  it('feeds elk the fixed grid-multiple node sizes and preserves them', async () => {
    const v = version(seq(httpStep('a'), endStep('z')));
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    // Activity node with a summary: NODE_WIDTH × ACTIVITY_NODE_HEIGHT.
    expect(nodes.get('root.steps[0]')).toMatchObject({
      width: NODE_WIDTH,
      height: ACTIVITY_NODE_HEIGHT,
    });
    // `end` renders header-only, so it is the compact height.
    expect(nodes.get('root.steps[1]')).toMatchObject({
      width: NODE_WIDTH,
      height: ACTIVITY_NODE_HEIGHT_COMPACT,
    });
    expect(nodes.get('start')).toMatchObject({ width: START_WIDTH, height: START_HEIGHT });
  });

  it('spaces sequential steps vertically by 3×GRID', async () => {
    const v = version(seq(httpStep('a'), setStep('b')));
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    const s0 = nodes.get('root.steps[0]')!;
    const s1 = nodes.get('root.steps[1]')!;
    const vGap = s1.position.y - (s0.position.y + (s0.height ?? 0));
    expect(vGap).toBe(3 * GRID);
  });

  it('spaces LTR regions horizontally by 2×GRID (half the old 64, tighter than vertical)', async () => {
    const v = version(seq(parallelStep('p', [seq(httpStep('a')), seq(setStep('b'))])));
    const nodes = byId((await layoutGraph(astToGraph(v))).nodes);
    const l0 = nodes.get('root.steps[0].parallel.branches[0]')!;
    const l1 = nodes.get('root.steps[0].parallel.branches[1]')!;
    const hGap = l1.position.x - (l0.position.x + (l0.width ?? 0));
    expect(hGap).toBe(2 * GRID);
    // Horizontal spacing is deliberately tighter than the 3×GRID vertical gap.
    expect(hGap).toBeLessThan(3 * GRID);
  });
});
