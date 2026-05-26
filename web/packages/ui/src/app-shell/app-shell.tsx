'use client';

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
} from '@pivox/primitives/sidebar';
import * as React from 'react';

import { AppShellContext } from './app-shell.context';
import { AppShellNavMain } from './nav-main';
import { AppShellNavSpaces } from './nav-spaces';
import { AppShellNavUser } from './nav-user';
import { AppShellOrgPicker } from './org-picker';

import type { AppShellContextValue } from './app-shell.types';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function AppShellProvider({
  value,
  children,
}: {
  value: AppShellContextValue;
  children: React.ReactNode;
}) {
  return <AppShellContext value={value}>{children}</AppShellContext>;
}

/* ------------------------------------------------------------------ */
/*  Sidebar — pre-composed default                                    */
/* ------------------------------------------------------------------ */

/**
 * Pre-composed sidebar: org picker in the header, nav-main + spaces
 * in the content, user menu in the footer. Most consumers want this.
 *
 * For custom composition, drop down to the individual pieces
 * (AppShell.OrgPicker / NavMain / NavSpaces / NavUser) inside your
 * own Sidebar/SidebarHeader/etc.
 */
function AppShellSidebar(props: React.ComponentProps<typeof Sidebar>) {
  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <AppShellOrgPicker />
      </SidebarHeader>
      <SidebarContent>
        <AppShellNavMain />
        <AppShellNavSpaces />
      </SidebarContent>
      <SidebarFooter>
        <AppShellNavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const AppShell = {
  Provider: AppShellProvider,
  Sidebar: AppShellSidebar,
  OrgPicker: AppShellOrgPicker,
  NavMain: AppShellNavMain,
  NavSpaces: AppShellNavSpaces,
  NavUser: AppShellNavUser,
  Context: AppShellContext,
};
