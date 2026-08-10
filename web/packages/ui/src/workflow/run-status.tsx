import { Badge } from '@pivox/primitives/badge';
import { cn } from '@pivox/primitives/utils';
import {
  Ban,
  Circle,
  CircleCheck,
  CircleX,
  Clock,
  LoaderCircle,
  type LucideIcon,
} from 'lucide-react';
import type { ComponentProps, ReactNode } from 'react';

/** The run/step lifecycle states surfaced in the UI (`STATE_UNSPECIFIED` excluded). */
export type RunState =
  'PENDING' | 'RUNNING' | 'WAITING' | 'SUCCEEDED' | 'FAILED' | 'CANCELLED';

type BadgeVariant = ComponentProps<typeof Badge>['variant'];

export type RunStatusMeta = {
  label: string;
  icon: LucideIcon;
  variant: BadgeVariant;
  /** Tailwind classes tinting the badge + node overlay for this state. */
  tone: string;
  /** Whether the icon animates (RUNNING). */
  spin: boolean;
};

export const runStatusMeta: Record<RunState, RunStatusMeta> = {
  PENDING: {
    label: 'Pending',
    icon: Circle,
    variant: 'outline',
    tone: 'text-muted-foreground border-border',
    spin: false,
  },
  RUNNING: {
    label: 'Running',
    icon: LoaderCircle,
    variant: 'outline',
    tone: 'text-primary border-primary/50 bg-primary/10',
    spin: true,
  },
  WAITING: {
    label: 'Waiting',
    icon: Clock,
    variant: 'outline',
    tone: 'text-amber-600 border-amber-500/50 bg-amber-500/10 dark:text-amber-400',
    spin: false,
  },
  SUCCEEDED: {
    label: 'Succeeded',
    icon: CircleCheck,
    variant: 'outline',
    tone: 'text-emerald-600 border-emerald-500/50 bg-emerald-500/10 dark:text-emerald-400',
    spin: false,
  },
  FAILED: {
    label: 'Failed',
    icon: CircleX,
    variant: 'destructive',
    tone: 'text-destructive border-destructive/50 bg-destructive/10',
    spin: false,
  },
  CANCELLED: {
    label: 'Cancelled',
    icon: Ban,
    variant: 'secondary',
    tone: 'text-muted-foreground border-border bg-muted',
    spin: false,
  },
};

export type RunStatusProps = {
  state: RunState;
  className?: string;
};

export function RunStatus({ state, className }: RunStatusProps): ReactNode {
  const meta = runStatusMeta[state];
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} className={cn(meta.tone, className)}>
      <Icon className={cn(meta.spin && 'animate-spin')} aria-hidden />
      {meta.label}
    </Badge>
  );
}
