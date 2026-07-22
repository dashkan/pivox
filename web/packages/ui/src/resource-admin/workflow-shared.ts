import { parseResourceName } from '@pivox/client';

/** The workflow name's leaf id, or the empty string. */
export function workflowLeafId(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).workflows ?? '';
}

/** The space slug of a workflow name, or the empty string for an org-direct one. */
export function workflowSpaceSlug(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).spaces ?? '';
}

/**
 * The live-version label: the `versions` leaf of the promoted WorkflowVersion
 * resource name, or an em dash when no version is promoted yet.
 */
export function workflowVersionLabel(version: string | undefined): string {
  if (!version) return '—';
  return parseResourceName(version).versions ?? '—';
}
