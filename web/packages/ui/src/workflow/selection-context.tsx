'use client';

import { createContext, use, useMemo, useState, type ReactNode } from 'react';

// Shared selection state for the canvas + config panel. The provider owns the
// selected node id; nodes and the panel read it via `use(SelectionContext)`.
// Lifting selection here (rather than into React Flow's own node.selected) keeps
// the panel, the node highlight, and the external `onNodeSelect` callback in one
// place.

type SelectionContextValue = {
  selectedNodeId: string | undefined;
  select: (nodeId: string | undefined) => void;
};

const SelectionContext = createContext<SelectionContextValue | undefined>(undefined);

export type SelectionProviderProps = {
  children: ReactNode;
};

export function SelectionProvider({ children }: SelectionProviderProps): ReactNode {
  const [selectedNodeId, setSelectedNodeId] = useState<string | undefined>(undefined);
  const value = useMemo<SelectionContextValue>(
    () => ({ selectedNodeId, select: setSelectedNodeId }),
    [selectedNodeId],
  );
  return <SelectionContext value={value}>{children}</SelectionContext>;
}

export function useSelection(): SelectionContextValue {
  const ctx = use(SelectionContext);
  if (!ctx) {
    throw new Error('useSelection must be used within a SelectionProvider');
  }
  return ctx;
}
