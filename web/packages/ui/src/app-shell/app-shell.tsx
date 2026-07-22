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
import { AppShellNavUser } from './nav-user';
import { AppShellScopePickerConnected } from './scope-picker-connected';

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
 * Pre-composed sidebar: scope picker (org + space) in the header,
 * nav-main in the content, user menu in the footer. Most consumers
 * want this.
 *
 * For custom composition, drop down to the individual pieces
 * (AppShell.ScopePicker / NavMain / NavUser) inside your own
 * Sidebar/SidebarHeader/etc.
 */
function AppShellSidebar(props: React.ComponentProps<typeof Sidebar>) {
  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <AppShellScopePickerConnected />
      </SidebarHeader>
      <SidebarContent>
        <AppShellNavMain />
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
  ScopePicker: AppShellScopePickerConnected,
  NavMain: AppShellNavMain,
  NavUser: AppShellNavUser,
  Context: AppShellContext,
};
