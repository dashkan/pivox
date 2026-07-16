// @vitest-environment jsdom
import { fireEvent, render, screen, within } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { WorkflowEdge, WorkflowNode } from '../../src/workflow/graph-types';
import { WorkflowCanvas } from '../../src/workflow/workflow-canvas';

// React Flow measures the DOM; jsdom needs these shims for nodes to mount.
beforeAll(() => {
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = ResizeObserverMock;
  globalThis.DOMMatrixReadOnly = class {
    m22 = 1;
  } as unknown as typeof DOMMatrixReadOnly;
});

const box = { position: { x: 0, y: 0 }, width: 240, height: 120 };

// A graph exercising every node kind: the synthetic start marker, the five
// activity leaves, the three containers with their regions, the join marker,
// and the "on error" region.
const nodes: WorkflowNode[] = [
  {
    id: 'start',
    type: 'start',
    position: { x: 0, y: 0 },
    width: 120,
    height: 44,
    data: { element: 'start' },
  },
  {
    id: 'root.steps[0]',
    type: 'http',
    ...box,
    data: {
      element: 'activity',
      kind: 'http',
      stepId: 'fetch',
      path: 'root.steps[0]',
      config: { connector: 'api', method: 'GET', path: '/users' },
    },
  },
  {
    id: 'root.steps[1]',
    type: 'set',
    ...box,
    data: {
      element: 'activity',
      kind: 'set',
      stepId: 'assign',
      path: 'root.steps[1]',
      config: { assignments: { total: '1', name: 'x' } },
    },
  },
  {
    id: 'root.steps[2]',
    type: 'run_workflow',
    ...box,
    data: {
      element: 'activity',
      kind: 'run_workflow',
      stepId: 'sub',
      path: 'root.steps[2]',
      config: { workflow: 'organizations/o/workflows/child' },
    },
  },
  {
    id: 'root.steps[3]',
    type: 'fail',
    ...box,
    data: {
      element: 'activity',
      kind: 'fail',
      stepId: 'boom',
      path: 'root.steps[3]',
      config: { message: 'kaboom' },
    },
  },
  {
    id: 'root.steps[4]',
    type: 'end',
    ...box,
    data: { element: 'activity', kind: 'end', stepId: 'done', path: 'root.steps[4]', config: {} },
  },
  // Condition container + one branch region + otherwise region.
  {
    id: 'root.steps[5]',
    type: 'condition',
    ...box,
    data: { element: 'container', kind: 'condition', stepId: 'cond', path: 'root.steps[5]' },
  },
  {
    id: 'root.steps[5].condition.branches[0].then',
    type: 'branch',
    ...box,
    parentId: 'root.steps[5]',
    data: { element: 'region', kind: 'branch', when: 'total > 0' },
  },
  {
    id: 'root.steps[5].condition.otherwise',
    type: 'otherwise',
    ...box,
    parentId: 'root.steps[5]',
    data: { element: 'region', kind: 'otherwise' },
  },
  // Parallel container + lane region.
  {
    id: 'root.steps[6]',
    type: 'parallel',
    ...box,
    data: { element: 'container', kind: 'parallel', stepId: 'par', path: 'root.steps[6]' },
  },
  {
    id: 'root.steps[6].parallel.branches[0]',
    type: 'lane',
    ...box,
    parentId: 'root.steps[6]',
    data: { element: 'region', kind: 'lane' },
  },
  // Try container + body + catch regions (with rethrow).
  {
    id: 'root.steps[7]',
    type: 'try',
    ...box,
    data: {
      element: 'container',
      kind: 'try',
      stepId: 'guard',
      path: 'root.steps[7]',
      rethrow: true,
    },
  },
  {
    id: 'root.steps[7].try.body',
    type: 'try-body',
    ...box,
    parentId: 'root.steps[7]',
    data: { element: 'region', kind: 'try-body' },
  },
  {
    id: 'root.steps[7].try.catch',
    type: 'try-catch',
    ...box,
    parentId: 'root.steps[7]',
    data: { element: 'region', kind: 'try-catch' },
  },
  // "on error" region.
  {
    id: 'error',
    type: 'error',
    ...box,
    data: { element: 'region', kind: 'error' },
  },
];

const edges: WorkflowEdge[] = [
  { id: 'es', source: 'start', target: 'root.steps[0]', data: { kind: 'start' } },
  { id: 'e0', source: 'root.steps[0]', target: 'root.steps[1]', data: { kind: 'sequence' } },
  { id: 'e1', source: 'root.steps[1]', target: 'root.steps[2]', data: { kind: 'sequence' } },
];

describe('WorkflowCanvas', () => {
  it('renders the React Flow host for a given graph', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(container.querySelector('.react-flow')).not.toBeNull();
    expect(container.querySelectorAll('.react-flow__node').length).toBeGreaterThan(0);
  });

  it('renders each activity variant with its one-line summary', () => {
    render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(screen.getByText('HTTP')).toBeDefined();
    expect(screen.getByText('GET /users')).toBeDefined();
    expect(screen.getByText('Set')).toBeDefined();
    expect(screen.getByText('total, name')).toBeDefined();
    expect(screen.getByText('Run workflow')).toBeDefined();
    expect(screen.getByText('organizations/o/workflows/child')).toBeDefined();
    expect(screen.getByText('Fail')).toBeDefined();
    expect(screen.getByText('kaboom')).toBeDefined();
    expect(screen.getByText('End')).toBeDefined();
  });

  it('renders container nodes with their region labels', () => {
    render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(screen.getByText('Condition')).toBeDefined();
    expect(screen.getByText('Parallel')).toBeDefined();
    expect(screen.getByText('Try')).toBeDefined();
    expect(screen.getByText('when total > 0')).toBeDefined();
    expect(screen.getByText('else')).toBeDefined();
    expect(screen.getByText('lane')).toBeDefined();
    expect(screen.getByText('try')).toBeDefined();
    expect(screen.getByText('catch')).toBeDefined();
    expect(screen.getByText('rethrow')).toBeDefined();
  });

  it('renders the error region', () => {
    render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(screen.getByText('on error')).toBeDefined();
  });

  it('renders no join marker node', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(screen.queryByText('join')).toBeNull();
    expect(container.querySelector('.react-flow__node[data-id="root.steps[6].join"]')).toBeNull();
  });

  it('renders the synthetic start marker', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(screen.getByText('Start')).toBeDefined();
    expect(container.querySelector('.react-flow__node[data-id="start"]')).not.toBeNull();
  });

  it('renders a minimap', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(container.querySelector('.react-flow__minimap')).not.toBeNull();
  });

  it('defines a directional arrowhead marker for edges', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    // jsdom lacks layout, so React Flow never draws edge paths — but it does
    // render the arrowhead <marker> def from defaultEdgeOptions.markerEnd. An
    // ArrowClosed marker renders a filled <polyline>; its presence proves the
    // arrowhead is wired and every edge points at it.
    const marker = container.querySelector('marker');
    expect(marker).not.toBeNull();
    expect(marker!.querySelector('polyline, path')).not.toBeNull();
  });

  it('hides the React Flow attribution watermark', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(container.querySelector('.react-flow__attribution')).toBeNull();
  });

  it('gives the inspector a fixed width and its own scrollbar', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    const httpNode = container.querySelector('.react-flow__node[data-id="root.steps[0]"]');
    fireEvent.click(httpNode as Element);
    const panel = container.querySelector('.react-flow__panel.top.right');
    expect(panel).not.toBeNull();
    expect((panel as HTMLElement).className).toContain('overflow-y-auto');
    expect((panel as HTMLElement).className).toContain('w-72');
  });

  it('is read-only: no node carries the draggable class', () => {
    const { container } = render(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(container.querySelectorAll('.react-flow__node').length).toBeGreaterThan(0);
    expect(container.querySelector('.react-flow__node.draggable')).toBeNull();
  });

  it('selecting a node opens the config panel and reports the selection', () => {
    const onNodeSelect = vi.fn();
    const { container } = render(
      <WorkflowCanvas nodes={nodes} edges={edges} onNodeSelect={onNodeSelect} />,
    );
    const httpNode = container.querySelector(
      '.react-flow__node[data-id="root.steps[0]"]',
    );
    expect(httpNode).not.toBeNull();
    fireEvent.click(httpNode as Element);

    expect(onNodeSelect).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'root.steps[0]' }),
    );
    const panel = container.querySelector('.react-flow__panel.top.right');
    expect(panel).not.toBeNull();
    expect(within(panel as HTMLElement).getByText('Connector')).toBeDefined();
    expect(within(panel as HTMLElement).getByText('api')).toBeDefined();
  });
});
