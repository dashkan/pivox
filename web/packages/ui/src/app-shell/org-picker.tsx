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
import { ChevronsUpDownIcon, PlusIcon } from 'lucide-react';
import * as React from 'react';

/**
 * Shape an org needs to be displayable in the picker. Maps to the
 * subset of fields the AccountOrganization proto we read from
 * `/v1/accounts/me/organizations` actually carries. `logo` is
 * client-side derived (icon or initials) — the API doesn't ship one.
 */
export interface OrgPickerOrg {
  /** Resource name, e.g. "organizations/acme". Used as the stable id. */
  organization: string;
  displayName: string;
  logo?: React.ReactNode;
}

/**
 * Sidebar header picker. Renders the active org with a dropdown of
 * the caller's other orgs + a "Create organization" CTA.
 *
 * Controlled component — `activeOrganization` + `onChangeOrganization`
 * are owned by the feature hook so the selection survives navigations
 * and persists to localStorage. `onCreateOrganization` is the CTA
 * handler (typically navigates to /auth/create-org).
 */
export function OrgPicker({
  orgs,
  activeOrganization,
  onChangeOrganization,
  onCreateOrganization,
}: {
  orgs: OrgPickerOrg[];
  activeOrganization: string;
  onChangeOrganization: (organization: string) => void;
  onCreateOrganization: () => void;
}) {
  const { isMobile } = useSidebar();
  const active = orgs.find((o) => o.organization === activeOrganization);

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
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                {active.logo}
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
            {orgs.map((org) => (
              <DropdownMenuItem
                key={org.organization}
                onClick={() => {
                  onChangeOrganization(org.organization);
                }}
                className="gap-2 p-2"
              >
                <div className="flex size-6 items-center justify-center rounded-md border">
                  {org.logo}
                </div>
                {org.displayName}
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={onCreateOrganization}
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
