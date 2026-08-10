'use client';

import { Avatar, AvatarFallback } from '@pivox/primitives/avatar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@pivox/primitives/dropdown-menu';
import { SidebarMenuButton, useSidebar } from '@pivox/primitives/sidebar';
import { ChevronsUpDownIcon, LogOutIcon, UserIcon } from 'lucide-react';
import { useState } from 'react';

import { useAppShellContext } from './app-shell.context';

/**
 * Shape the user needs to be displayable in the menu. Matches the
 * relevant subset of the authenticated user profile (or our hydrated
 * `ServerSession`) — name + email + avatar URL.
 */
export interface NavUserUser {
  displayName: string | null;
  email: string | null;
  photoURL: string | null;
}

/**
 * The user's photo, or their initials once we know the photo won't load.
 *
 * Plain <img>, not <AvatarImage>: AvatarImage verifies the URL loads in a
 * layout effect, which doesn't run during SSR — so SSR would render the
 * initials and hydration would swap to the image, which flashes. Rendering
 * the <img> directly puts it in the SSR HTML, at the cost of owning the error
 * path: provider photo URLs are reliable but not guaranteed, and without
 * `onError` a failed load leaves the browser's broken-image icon (plus
 * overflowing alt text) in the sidebar until the next reload.
 *
 * Failure is tracked per URL, not as a sticky boolean, so switching photos
 * gets a fresh attempt.
 */
function NavUserAvatar({
  photoURL,
  name,
  initials,
}: {
  photoURL: string | null;
  name: string;
  initials: string;
}) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);

  return (
    <Avatar className="h-8 w-8 rounded-lg">
      {photoURL && photoURL !== failedSrc ? (
        <img
          src={photoURL}
          alt={name}
          onError={() => setFailedSrc(photoURL)}
          className="aspect-square size-full rounded-lg object-cover"
        />
      ) : (
        <AvatarFallback className="rounded-lg">{initials}</AvatarFallback>
      )}
    </Avatar>
  );
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
      <DropdownMenuTrigger
        render={
          <SidebarMenuButton
            size="lg"
            className="data-open:bg-sidebar-accent data-open:text-sidebar-accent-foreground"
          >
            <NavUserAvatar
              photoURL={user.photoURL}
              name={user.displayName ?? 'User avatar'}
              initials={initials}
            />
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
        }
      />
      <DropdownMenuContent
        className="w-(--anchor-width) min-w-56 rounded-lg"
        side={isMobile ? 'bottom' : 'right'}
        align="end"
        sideOffset={4}
      >
        {/* Base UI GroupLabel needs a Group ancestor (Radix allowed bare). */}
        <DropdownMenuGroup>
          <DropdownMenuLabel className="p-0 font-normal">
            <div className="flex items-center gap-2 px-1 py-1.5 text-start text-sm">
              <NavUserAvatar
                photoURL={user.photoURL}
                name={user.displayName ?? 'User avatar'}
                initials={initials}
              />
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
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => {
            // Prefer the injected handler (web → Keycloak account console);
            // otherwise open the in-app profile dialog (Electron).
            if (actions.openAccount) {
              actions.openAccount();
            } else {
              actions.setProfileOpen(true);
            }
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
