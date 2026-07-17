// @vitest-environment jsdom
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';

import type { WorkflowEdge, WorkflowNode } from '../../src/workflow/graph-types';

// Stub the theme accessor so the test drives the app's selected theme
// directly, and capture the `colorMode` the canvas forwards to React Flow
// (via the Canvas primitive, which spreads it straight onto <ReactFlow>).
// The Canvas mock returns null so React Flow's DOM-measuring internals never
// mount — this test asserts the wiring, not the render.
const useThemeMock = vi.fn<() => 'light' | 'dark' | 'system'>();
vi.mock('@/theme-switcher/use-theme', () => ({
  useTheme: () => useThemeMock(),
}));

const colorModeSpy = vi.fn<(mode: unknown) => void>();
vi.mock('@pivox/primitives/canvas', () => ({
  Canvas: ({ colorMode }: { colorMode?: unknown; children?: ReactNode }): null => {
    colorModeSpy(colorMode);
    return null;
  },
}));

// Import after the mocks are registered so the module graph picks them up.
const { WorkflowCanvas } = await import('../../src/workflow/workflow-canvas');

const nodes: WorkflowNode[] = [
  {
    id: 'start',
    type: 'start',
    position: { x: 0, y: 0 },
    width: 120,
    height: 44,
    data: { element: 'start' },
  },
];
const edges: WorkflowEdge[] = [];

describe('WorkflowCanvas color mode', () => {
  it.each(['light', 'dark', 'system'] as const)(
    'forwards the app theme %s to React Flow colorMode',
    (theme) => {
      useThemeMock.mockReturnValue(theme);
      colorModeSpy.mockClear();

      render(<WorkflowCanvas nodes={nodes} edges={edges} />);

      // The app's stored selection is the single source of truth — an explicit
      // light/dark choice reaches React Flow verbatim, and 'system' is passed
      // through (React Flow's own matchMedia resolution then agrees with ours).
      expect(colorModeSpy).toHaveBeenCalledWith(theme);
    },
  );
});
