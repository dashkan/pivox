'use client';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@pivox/primitives/dropdown-menu';
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@pivox/primitives/sidebar';
import { Link } from '@tanstack/react-router';
import {
  ArrowRightIcon,
  FolderIcon,
  MoreHorizontalIcon,
  Trash2Icon,
} from 'lucide-react';

import { useAppShellContext } from './app-shell.context';

/**
 * Shape a space needs to be listable in the sidebar group. Maps to
 * the subset of fields the Space proto from `/v1/organizations/
 * {organization}/spaces` actually carries. `icon` is client-side
 * derived — the API doesn't ship one.
 *
 * `href` is the absolute path to the space landing page,
 * constructed by the feature hook (typically `/spaces/<slug>`).
 */
export interface NavSpacesSpace {
  /** Resource name, e.g. "organizations/acme/spaces/dev". */
  space: string;
  displayName: string;
  href: string;
  icon?: React.ReactNode;
}

/**
 * Sidebar group listing the caller's spaces in the active
 * organization. Hidden when the sidebar is collapsed to icon mode
 * (spaces don't have meaningful single-icon shorthand).
 *
 * The per-item menu (View / Share / Delete) is intentionally inert
 * placeholder content — wiring those actions is a later iteration
 * once the underlying space operations exist.
 */
export function AppShellNavSpaces() {
  const { state } = useAppShellContext();
  const { isMobile } = useSidebar();

  return (
    <SidebarGroup className="group-data-[collapsible=icon]:hidden">
      <SidebarGroupLabel>Spaces</SidebarGroupLabel>
      <SidebarMenu>
        {state.spaces.map((sp) => (
          <SidebarMenuItem key={sp.space}>
            <SidebarMenuButton asChild>
              <Link to={sp.href}>
                {sp.icon}
                <span>{sp.displayName}</span>
              </Link>
            </SidebarMenuButton>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <SidebarMenuAction
                  showOnHover
                  className="aria-expanded:bg-muted"
                >
                  <MoreHorizontalIcon />
                  <span className="sr-only">More</span>
                </SidebarMenuAction>
              </DropdownMenuTrigger>
              <DropdownMenuContent
                className="w-48 rounded-lg"
                side={isMobile ? 'bottom' : 'right'}
                align={isMobile ? 'end' : 'start'}
              >
                <DropdownMenuItem>
                  <FolderIcon className="text-muted-foreground" />
                  <span>View Space</span>
                </DropdownMenuItem>
                <DropdownMenuItem>
                  <ArrowRightIcon className="text-muted-foreground" />
                  <span>Share Space</span>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem>
                  <Trash2Icon className="text-muted-foreground" />
                  <span>Delete Space</span>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        ))}
        <SidebarMenuItem>
          <SidebarMenuButton className="text-sidebar-foreground/70">
            <MoreHorizontalIcon className="text-sidebar-foreground/70" />
            <span>More</span>
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarGroup>
  );
}
