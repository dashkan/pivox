'use client';

import {
  DropdownMenu,
  DropdownMenuContent,
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
import { ChevronsUpDownIcon, PlusIcon } from 'lucide-react';
import * as React from 'react';

import { useAppShellContext } from './app-shell.context';

/**
 * Shape an org needs to be displayable in the picker. Maps to the
 * subset of fields the AccountOrganization proto we read from
 * `/v1/accounts/me/organizations` actually carries.
 *
 * `logo` is optional — providers leave it unset until the AIP org
 * resource ships a logo field, and the picker derives a fallback
 * (first two letters of displayName, rendered in the colored
 * wrapper) for missing values. Same pattern as the avatar fallback
 * in AppShellNavUser.
 */
export interface OrgPickerOrg {
  /** Resource name, e.g. "organizations/acme". Used as the stable id. */
  organization: string;
  displayName: string;
  logo?: React.ReactNode;
}

/**
 * First two letters of the display name, uppercased. Used as the
 * fallback logo when `logo` isn't set on an OrgPickerOrg. Falls back
 * to "?" if displayName is empty / only whitespace.
 */
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
 * Sidebar header picker. Renders the active org with a dropdown of
 * the caller's other orgs + a "Create Organization" CTA.
 *
 * Consumes AppShellContext for state.orgs / state.activeOrganization
 * / actions.setActiveOrganization / actions.createOrganization — the
 * provider owns persistence (localStorage), data fetching, and the
 * navigate target.
 */
export function AppShellOrgPicker() {
  const { state, actions } = useAppShellContext();
  const { isMobile } = useSidebar();
  const active = state.orgs.find(
    (o) => o.organization === state.activeOrganization,
  );

  // Loading shape: render the same physical layout the active picker
  // uses so the sidebar header doesn't shift size when data arrives.
  // Skeleton matches the size-8 logo block + the two text rows the
  // trigger would render.
  if (!active && state.orgsLoading) {
    return (
      <SidebarMenu>
        <SidebarMenuItem>
          {/* oxlint-disable jsx-a11y/prefer-tag-over-role -- SidebarMenuButton is a styled design-system component that cannot render as <output>; role="status" is the correct live-region role for this loading placeholder, paired with aria-busy + aria-label. */}
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
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sm font-medium text-sidebar-primary-foreground">
                {active.logo ?? orgInitials(active.displayName)}
              </div>
              <div className="grid flex-1 text-start text-sm leading-tight">
                <span className="truncate font-medium">
                  {active.displayName}
                </span>
              </div>
              <ChevronsUpDownIcon className="ms-auto" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg"
            align="start"
            side={isMobile ? 'bottom' : 'right'}
            sideOffset={4}
          >
            <DropdownMenuLabel className="text-xs text-muted-foreground">
              Organizations
            </DropdownMenuLabel>
            {state.orgs.map((org) => (
              <DropdownMenuItem
                key={org.organization}
                onClick={() => {
                  actions.setActiveOrganization(org.organization);
                }}
                className="gap-2 p-2"
              >
                <div className="flex size-6 items-center justify-center rounded-md border text-xs font-medium">
                  {org.logo ?? orgInitials(org.displayName)}
                </div>
                {org.displayName}
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={actions.createOrganization}
              className="gap-2 p-2"
            >
              <div className="flex size-6 items-center justify-center rounded-md border bg-transparent">
                <PlusIcon className="size-4" />
              </div>
              <div className="font-medium text-muted-foreground">
                Create Organization
              </div>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
