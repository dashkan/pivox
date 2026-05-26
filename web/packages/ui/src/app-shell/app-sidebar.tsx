'use client';

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
} from '@pivox/primitives/sidebar';
import {
  AudioLinesIcon,
  BookOpenIcon,
  BotIcon,
  GalleryVerticalEndIcon,
  Settings2Icon,
  TerminalIcon,
  TerminalSquareIcon,
} from 'lucide-react';
import * as React from 'react';

import { NavMain } from './nav-main';
import type { NavMainItem } from './nav-main';
import { NavSpaces } from './nav-spaces';
import type { NavSpacesSpace } from './nav-spaces';
import { NavUser } from './nav-user';
import type { NavUserUser } from './nav-user';
import { OrgPicker } from './org-picker';
import type { OrgPickerOrg } from './org-picker';

/**
 * Sample data — populated by the feature hook in Stage B2. Kept
 * inline for now so the shell renders standalone in Stage B1 (no
 * data wiring) and isolated stories/screenshots stay self-contained.
 *
 * Each block carries the same shape the controlled props expect, so
 * the swap to real data in Stage B2 is a pure delete-and-thread.
 */
const sampleOrgs: OrgPickerOrg[] = [
  {
    organization: 'organizations/acme',
    displayName: 'Acme Inc',
    logo: <GalleryVerticalEndIcon />,
  },
  {
    organization: 'organizations/acme-corp',
    displayName: 'Acme Corp.',
    logo: <AudioLinesIcon />,
  },
  {
    organization: 'organizations/evil',
    displayName: 'Evil Corp.',
    logo: <TerminalIcon />,
  },
];

const sampleNavMain: NavMainItem[] = [
  {
    title: 'Playground',
    href: '/',
    icon: <TerminalSquareIcon />,
    isActive: true,
    items: [
      { title: 'History', href: '/' },
      { title: 'Starred', href: '/' },
      { title: 'Settings', href: '/' },
    ],
  },
  {
    title: 'Models',
    href: '/',
    icon: <BotIcon />,
    items: [
      { title: 'Genesis', href: '/' },
      { title: 'Explorer', href: '/' },
      { title: 'Quantum', href: '/' },
    ],
  },
  {
    title: 'Documentation',
    href: '/',
    icon: <BookOpenIcon />,
    items: [
      { title: 'Introduction', href: '/' },
      { title: 'Get Started', href: '/' },
      { title: 'Tutorials', href: '/' },
      { title: 'Changelog', href: '/' },
    ],
  },
  {
    title: 'Settings',
    href: '/',
    icon: <Settings2Icon />,
    items: [
      { title: 'General', href: '/' },
      { title: 'Team', href: '/' },
      { title: 'Billing', href: '/' },
      { title: 'Limits', href: '/' },
    ],
  },
];

const sampleSpaces: NavSpacesSpace[] = [];

const sampleUser: NavUserUser = {
  displayName: 'shadcn',
  email: '[email protected]',
  photoURL: null,
};

/**
 * Top-level sidebar composition: org picker in the header, nav +
 * spaces list in the content, user menu in the footer. Collapses to
 * icons in compact mode (nav-spaces hides because workspaces don't
 * have meaningful single-icon shorthand).
 *
 * Stage B1 wires the controlled props to inline sample data so the
 * component renders in isolation. Stage B2 replaces the inline data
 * with a feature hook that fetches the user's orgs + active-org's
 * spaces and persists the active-org selection in localStorage.
 */
export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <OrgPicker
          orgs={sampleOrgs}
          activeOrganization={sampleOrgs[0]?.organization ?? ''}
          onChangeOrganization={() => {
            // wired in Stage B2
          }}
          onCreateOrganization={() => {
            // wired in Stage B2
          }}
        />
      </SidebarHeader>
      <SidebarContent>
        <NavMain items={sampleNavMain} />
        <NavSpaces spaces={sampleSpaces} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser
          user={sampleUser}
          onManageAccount={() => {
            // wired in Stage B2
          }}
          onSignOut={() => {
            // wired in Stage B2
          }}
        />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
