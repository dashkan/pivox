'use client';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@pivox/primitives/dropdown-menu';
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@pivox/primitives/sidebar';
import { Skeleton } from '@pivox/primitives/skeleton';
import { CheckIcon, ChevronsUpDownIcon } from 'lucide-react';

import type { NavSpacesSpace } from './nav-spaces';
import type { OrgPickerOrg } from './org-picker';

const ALL_SPACES_LABEL = 'All spaces';

export interface ScopePickerProps {
  orgs: OrgPickerOrg[];
  /** Resource name of the active org, e.g. "organizations/acme". */
  activeOrganization: string | null;
  /** Spaces in the active org. */
  spaces: NavSpacesSpace[];
  /** Resource name of the active space, or null for the org rollup. */
  activeSpace: string | null;
  orgsLoading?: boolean;
  onSelectOrganization: (organization: string) => void;
  /** null selects the org rollup ("All spaces"). */
  onSelectSpace: (space: string | null) => void;
}

/** First two initials of the display name, uppercased; "?" when empty. */
function orgInitials(displayName: string): string {
  const initials = displayName
    .split(/\s+/)
    .map((p) => p[0])
    .filter(Boolean)
    .slice(0, 2)
    .join('')
    .toUpperCase();
  return initials || '?';
}

/**
 * Sidebar header org + space selector. Replaces the org-only picker: one
 * control switches org and, within it, a space or "All spaces" (the org
 * rollup). Prop-driven and router-agnostic — the consumer injects the
 * navigation (each selection drives the URL, the single source of scope
 * truth). Always renders once an org is resolved, even for single-org callers.
 */
export function AppShellScopePicker({
  orgs,
  activeOrganization,
  spaces,
  activeSpace,
  orgsLoading = false,
  onSelectOrganization,
  onSelectSpace,
}: ScopePickerProps) {
  const { isMobile } = useSidebar();
  const active = orgs.find((o) => o.organization === activeOrganization);
  const scopeLabel =
    (activeSpace && spaces.find((s) => s.space === activeSpace)?.displayName) ||
    ALL_SPACES_LABEL;

  if (!active && orgsLoading) {
    return (
      <SidebarMenu>
        <SidebarMenuItem>
          {/* oxlint-disable jsx-a11y/prefer-tag-over-role -- styled design-system button can't be <output>; role=status is the correct live region here. */}
          <SidebarMenuButton
            size="lg"
            disabled
            className="pointer-events-none"
            role="status"
            aria-label="Loading organizations"
            aria-busy="true"
          >
            <Skeleton aria-hidden="true" className="size-8 rounded-lg" />
            <div aria-hidden="true" className="grid flex-1 gap-1">
              <Skeleton className="h-3 w-24" />
              <Skeleton className="h-2 w-16" />
            </div>
          </SidebarMenuButton>
          {/* oxlint-enable jsx-a11y/prefer-tag-over-role */}
        </SidebarMenuItem>
      </SidebarMenu>
    );
  }

  if (!active) return null;

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                size="lg"
                className="data-open:bg-sidebar-accent data-open:text-sidebar-accent-foreground"
              >
                <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sm font-medium text-sidebar-primary-foreground">
                  {active.logo ?? orgInitials(active.displayName)}
                </div>
                <div className="grid flex-1 text-start text-sm leading-tight">
                  <span className="truncate font-medium">
                    {active.displayName}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {scopeLabel}
                  </span>
                </div>
                <ChevronsUpDownIcon className="ms-auto" />
              </SidebarMenuButton>
            }
          />
          <DropdownMenuContent
            className="w-(--anchor-width) min-w-60 rounded-lg"
            align="start"
            side={isMobile ? 'bottom' : 'right'}
            sideOffset={4}
          >
            {/* Base UI's GroupLabel requires a Group ancestor (Radix allowed a
                bare label), so each labelled section is wrapped. */}
            <DropdownMenuGroup>
              <DropdownMenuLabel className="text-xs text-muted-foreground">
                Organizations
              </DropdownMenuLabel>
              {orgs.map((org) => (
                // closeOnClick={false} keeps the menu open so org + space can be
                // picked in one session (a context switcher applies immediately;
                // dismiss with Escape / click-away). Base UI has no onSelect —
                // that prop name is a native DOM text-selection handler here.
                <DropdownMenuItem
                  key={org.organization}
                  closeOnClick={false}
                  onClick={() => {
                    onSelectOrganization(org.organization);
                  }}
                  className="gap-2 p-2"
                >
                  <div className="flex size-6 items-center justify-center rounded-md border text-xs font-medium">
                    {org.logo ?? orgInitials(org.displayName)}
                  </div>
                  <span className="flex-1 truncate">{org.displayName}</span>
                  {org.organization === activeOrganization && (
                    <CheckIcon className="size-4" />
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel className="text-xs text-muted-foreground">
                Spaces
              </DropdownMenuLabel>
              <DropdownMenuItem
                onClick={() => {
                  onSelectSpace(null);
                }}
                className="gap-2 p-2"
              >
                <span className="flex-1 truncate">{ALL_SPACES_LABEL}</span>
                {activeSpace === null && <CheckIcon className="size-4" />}
              </DropdownMenuItem>
              {spaces.map((sp) => (
                <DropdownMenuItem
                  key={sp.space}
                  onClick={() => {
                    onSelectSpace(sp.space);
                  }}
                  className="gap-2 p-2"
                >
                  {sp.icon}
                  <span className="flex-1 truncate">{sp.displayName}</span>
                  {sp.space === activeSpace && <CheckIcon className="size-4" />}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
