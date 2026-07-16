import type { Actor } from './types';

/** Best-effort actor label: display name, then email, then a soft-deleted note. */
export function actorLabel(actor: Actor | undefined): string {
  if (!actor) return '—';
  if (actor.isDeleted) return 'Deleted user';
  return actor.displayName || actor.email || '—';
}

/** Locale date-time for an RFC 3339 timestamp; em-dash when absent/invalid. */
export function formatTimestamp(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString();
}
