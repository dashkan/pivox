'use client';

import { Avatar, AvatarFallback } from '@pivox/primitives/avatar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@pivox/primitives/dropdown-menu';
import { SidebarMenuButton, useSidebar } from '@pivox/primitives/sidebar';
import { ChevronsUpDownIcon, LogOutIcon, UserIcon } from 'lucide-react';

import { useAppShellContext } from './app-shell.context';

/**
 * Shape the user needs to be displayable in the menu. Matches the
 * relevant subset of Firebase's `User` (or our hydrated
 * `ServerSession`) — name + email + avatar URL.
 */
export interface NavUserUser {
  displayName: string | null;
  email: string | null;
  photoURL: string | null;
}

/**
 * Sidebar-footer user menu. Reconciles the visual shape of shadcn's
 * sidebar-07 `nav-user` block with the menu items previously in
 * `<AppLayoutHeaderAvatar>` — sample "Upgrade to Pro / Billing /
 * Notifications" items dropped, replaced with the two actions we
 * actually expose: open the profile dialog and sign out.
 *
 * Consumes AppShellContext for the user shape + setProfileOpen /
 * signOut actions. Returns null when there's no user (defensive
 * single-frame guard between sign-out completing and the route
 * gate's redirect committing).
 */
export function AppShellNavUser() {
  const { state, actions } = useAppShellContext();
  const { isMobile } = useSidebar();

  if (!state.user) return null;

  const user = state.user;
  const initials = (user.displayName ?? user.email ?? 'U')
    .split(/\s+/)
    .map((p) => p[0])
    .filter(Boolean)
    .slice(0, 2)
    .join('')
    .toUpperCase();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <SidebarMenuButton
          size="lg"
          className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
        >
          <Avatar className="h-8 w-8 rounded-lg">
            {user.photoURL ? (
              // Plain <img>, not <AvatarImage>. Radix's AvatarImage
              // uses useLayoutEffect to verify the URL loads before
              // rendering the <img>, but useLayoutEffect doesn't run
              // during SSR — so SSR renders the fallback initials, then
              // hydration swaps to the image. The flash is jarring and
              // pointless for our use case: Firebase photoURLs are
              // reliable OAuth-provider URLs (Google/Apple/GitHub).
              // Render the image directly so SSR HTML includes it.
              <img
                src={user.photoURL}
                alt={user.displayName ?? 'User avatar'}
                className="aspect-square size-full rounded-lg object-cover"
              />
            ) : (
              <AvatarFallback className="rounded-lg">{initials}</AvatarFallback>
            )}
          </Avatar>
          <div className="grid flex-1 text-start text-sm leading-tight">
            <span className="truncate font-medium">
              {user.displayName ?? 'User'}
            </span>
            {user.email ? (
              <span className="truncate text-xs">{user.email}</span>
            ) : null}
          </div>
          <ChevronsUpDownIcon className="ms-auto size-4" />
        </SidebarMenuButton>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        className="w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg"
        side={isMobile ? 'bottom' : 'right'}
        align="end"
        sideOffset={4}
      >
        <DropdownMenuLabel className="p-0 font-normal">
          <div className="flex items-center gap-2 px-1 py-1.5 text-start text-sm">
            <Avatar className="h-8 w-8 rounded-lg">
              {user.photoURL ? (
                <img
                  src={user.photoURL}
                  alt={user.displayName ?? 'User avatar'}
                  className="aspect-square size-full rounded-lg object-cover"
                />
              ) : (
                <AvatarFallback className="rounded-lg">
                  {initials}
                </AvatarFallback>
              )}
            </Avatar>
            <div className="grid flex-1 text-start text-sm leading-tight">
              <span className="truncate font-medium">
                {user.displayName ?? 'User'}
              </span>
              {user.email ? (
                <span className="truncate text-xs">{user.email}</span>
              ) : null}
            </div>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => {
            actions.setProfileOpen(true);
          }}
        >
          <UserIcon />
          Manage Account
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => {
            void actions.signOut();
          }}
        >
          <LogOutIcon />
          Sign Out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
