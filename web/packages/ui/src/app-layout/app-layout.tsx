'use client';

import { cn } from '@pivox/primitives/utils';
import { Button } from '@pivox/primitives/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@pivox/primitives/dropdown-menu';
import { Skeleton } from '@pivox/primitives/skeleton';
import { AppLayoutContext, useAppLayoutContext } from './app-layout.context';
import type { AppLayoutContextValue } from './app-layout.types';
import { UserAvatar } from '@/user-avatar/user-avatar';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function AppLayoutProvider({
  value,
  children,
}: {
  value: AppLayoutContextValue;
  children: React.ReactNode;
}) {
  return <AppLayoutContext value={value}>{children}</AppLayoutContext>;
}

/* ------------------------------------------------------------------ */
/*  Root                                                              */
/* ------------------------------------------------------------------ */

function AppLayoutRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={cn('flex min-h-screen flex-col', className)}>
      {children}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Header                                                            */
/* ------------------------------------------------------------------ */

function AppLayoutHeader({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <header
      className={cn(
        'sticky top-0 z-40 flex h-14 items-center border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60',
        className,
      )}
    >
      {children}
    </header>
  );
}

/* ------------------------------------------------------------------ */
/*  HeaderTitle                                                       */
/* ------------------------------------------------------------------ */

function AppLayoutHeaderTitle({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={cn('flex items-center gap-2 font-semibold', className)}>
      {children}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  HeaderNav — spacer + right-aligned items                          */
/* ------------------------------------------------------------------ */

function AppLayoutHeaderNav({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <nav className={cn('ml-auto flex items-center gap-2', className)}>
      {children}
    </nav>
  );
}

/* ------------------------------------------------------------------ */
/*  HeaderAvatar — avatar with dropdown menu                          */
/* ------------------------------------------------------------------ */

function AppLayoutHeaderAvatar({ className }: { className?: string }) {
  const { state, actions } = useAppLayoutContext();

  if (state.loading) {
    return <Skeleton className="size-8 rounded-full" />;
  }

  if (!state.user) {
    return (
      <Button size="sm" onClick={actions.navigateToLogin}>
        Sign in
      </Button>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className={cn('cursor-pointer rounded-full', className)}
        >
          <UserAvatar
            src={state.user.photoURL}
            name={state.user.displayName ?? state.user.email}
          />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <div className="px-2 py-1.5">
          <p className="text-sm font-medium">
            {state.user.displayName ?? 'User'}
          </p>
          <p className="text-xs text-muted-foreground">{state.user.email}</p>
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => actions.setProfileOpen(true)}>
          Manage account
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={actions.signOut}>Sign out</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/* ------------------------------------------------------------------ */
/*  Content                                                           */
/* ------------------------------------------------------------------ */

function AppLayoutContent({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return <main className={cn('flex-1', className)}>{children}</main>;
}

/* ------------------------------------------------------------------ */
/*  Sidebar                                                           */
/* ------------------------------------------------------------------ */

function AppLayoutSidebar({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  // TODO: Integrate with shadcn Sidebar primitive for collapsible,
  //       mobile sheet, keyboard shortcut behavior
  return (
    <aside className={cn('w-64 shrink-0 border-r', className)}>
      {children}
    </aside>
  );
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function AppLayoutFooter({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <footer
      className={cn(
        'flex items-center border-t px-4 py-3 text-sm text-muted-foreground',
        className,
      )}
    >
      {children}
    </footer>
  );
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const AppLayout = {
  Provider: AppLayoutProvider,
  Root: AppLayoutRoot,
  Header: AppLayoutHeader,
  HeaderTitle: AppLayoutHeaderTitle,
  HeaderNav: AppLayoutHeaderNav,
  HeaderAvatar: AppLayoutHeaderAvatar,
  Content: AppLayoutContent,
  Sidebar: AppLayoutSidebar,
  Footer: AppLayoutFooter,
  Context: AppLayoutContext,
};
