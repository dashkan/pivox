import { Panel } from '@pivox/primitives/panel';
import { useNodes } from '@xyflow/react';
import type { ReactNode } from 'react';

import type { ActivityNodeData, WorkflowNode, WorkflowNodeData } from './graph-types';
import { useSelection } from './selection-context';

// Presentational config inspector. Reads the shared selection, finds the node
// among the canvas nodes, and lays out its static `data` config as label/value
// rows. Runtime step output/error is layered on in T7.

type Row = { label: string; value: string; mono?: boolean };

const asJson = (value: unknown): string => JSON.stringify(value, null, 2);

function activityRows(data: ActivityNodeData): Row[] {
  switch (data.kind) {
    case 'http': {
      const c = data.config;
      const rows: Row[] = [
        { label: 'Connector', value: c.connector },
        { label: 'Method', value: c.method },
      ];
      if (c.path) rows.push({ label: 'Path', value: c.path, mono: true });
      if (c.query) rows.push({ label: 'Query', value: asJson(c.query), mono: true });
      if (c.headers) rows.push({ label: 'Headers', value: asJson(c.headers), mono: true });
      if (c.body) rows.push({ label: 'Body', value: c.body, mono: true });
      if (c.successStatus?.length) {
        rows.push({ label: 'Success status', value: c.successStatus.join(', ') });
      }
      if (c.retryableStatus?.length) {
        rows.push({ label: 'Retryable status', value: c.retryableStatus.join(', ') });
      }
      return rows;
    }
    case 'set':
      return Object.entries(data.config.assignments).map(([name, cel]) => ({
        label: name,
        value: cel,
        mono: true,
      }));
    case 'run_workflow': {
      const rows: Row[] = [{ label: 'Workflow', value: data.config.workflow }];
      if (data.config.parameters) {
        rows.push({ label: 'Parameters', value: asJson(data.config.parameters), mono: true });
      }
      return rows;
    }
    case 'fail':
      return data.config.message ? [{ label: 'Message', value: data.config.message }] : [];
    case 'end':
      return [];
    default: {
      const exhaustive: never = data;
      return exhaustive;
    }
  }
}

const activityTitle: Record<ActivityNodeData['kind'], string> = {
  http: 'HTTP',
  set: 'Set',
  run_workflow: 'Run workflow',
  fail: 'Fail',
  end: 'End',
};

function nodeView(data: WorkflowNodeData): { title: string; rows: Row[] } {
  switch (data.element) {
    case 'activity':
      return { title: activityTitle[data.kind], rows: activityRows(data) };
    case 'container': {
      const rows: Row[] = [];
      if (data.kind === 'try' && data.rethrow) rows.push({ label: 'Rethrow', value: 'true' });
      return { title: data.kind, rows };
    }
    case 'region':
      return {
        title: data.kind,
        rows: data.when ? [{ label: 'When', value: data.when, mono: true }] : [],
      };
    case 'start':
      return { title: 'Start', rows: [] };
    default: {
      const exhaustive: never = data;
      return exhaustive;
    }
  }
}

export function ConfigPanel(): ReactNode {
  const { selectedNodeId } = useSelection();
  const nodes = useNodes<WorkflowNode>();
  const node = nodes.find((n) => n.id === selectedNodeId);
  if (!node) return null;

  const { title, rows } = nodeView(node.data);
  const stepId = node.data.element === 'activity' || node.data.element === 'container'
    ? node.data.stepId
    : undefined;

  // Fixed width + capped height with its own scrollbar: the inspector never
  // grows with content or pushes the canvas layout.
  return (
    <Panel
      position="top-right"
      className="w-72 max-h-[calc(100%-2rem)] overflow-y-auto p-3"
    >
      <div className="mb-2">
        <p className="text-sm font-medium capitalize">{title}</p>
        {stepId ? <p className="font-mono text-xs text-muted-foreground">{stepId}</p> : null}
      </div>
      {rows.length > 0 ? (
        <dl className="grid gap-1.5">
          {rows.map((row) => (
            <div key={row.label} className="grid gap-0.5">
              <dt className="text-xs text-muted-foreground">{row.label}</dt>
              <dd
                className={
                  row.mono ? 'font-mono text-xs whitespace-pre-wrap break-all' : 'text-sm'
                }
              >
                {row.value}
              </dd>
            </div>
          ))}
        </dl>
      ) : (
        <p className="text-xs text-muted-foreground">No configuration.</p>
      )}
    </Panel>
  );
}
