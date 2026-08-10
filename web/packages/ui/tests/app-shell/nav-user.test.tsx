// @vitest-environment jsdom
import { SidebarProvider } from '@pivox/primitives/sidebar';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { AppShellContext } from '../../src/app-shell/app-shell.context';
import { AppShellNavUser } from '../../src/app-shell/nav-user';

import type { AppShellContextValue } from '../../src/app-shell/app-shell.types';
import type { NavUserUser } from '../../src/app-shell/nav-user';

// SidebarProvider (useIsMobile) touches DOM APIs jsdom lacks.
beforeAll(() => {
  window.matchMedia ??= vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
});

const PHOTO = 'https://lh3.googleusercontent.com/a/abc=s96-c';

function tree(user: NavUserUser) {
  const value = {
    state: {
      user,
      orgs: [],
      orgsLoading: false,
      activeOrganization: null,
      activeSpace: null,
      spaces: [],
      spacesLoading: false,
      navMain: [],
      profileOpen: false,
    },
    actions: {
      setActiveOrganization: vi.fn(),
      selectSpace: vi.fn(),
      createOrganization: vi.fn(),
      setProfileOpen: vi.fn(),
      signOut: vi.fn(),
    },
  } satisfies AppShellContextValue;

  return (
    <AppShellContext.Provider value={value}>
      <SidebarProvider>
        <AppShellNavUser />
      </SidebarProvider>
    </AppShellContext.Provider>
  );
}

const renderNavUser = (user: NavUserUser) => render(tree(user));

const USER: NavUserUser = {
  displayName: 'Ashkan Daie',
  email: 'ashkan.daie@gmail.com',
  photoURL: PHOTO,
};

describe('AppShellNavUser avatar', () => {
  it('renders the photo directly so SSR HTML includes it', () => {
    renderNavUser(USER);
    expect(screen.getByAltText('Ashkan Daie')).toHaveProperty(
      'src',
      PHOTO,
    );
    expect(screen.queryByText('AD')).toBeNull();
  });

  it('falls back to initials when the photo fails to load', () => {
    renderNavUser(USER);
    // Provider photo URLs are rate-limited (lh3.googleusercontent.com
    // answers 429), which must not leave a broken-image icon on screen.
    fireEvent.error(screen.getByAltText('Ashkan Daie'));

    expect(screen.queryByAltText('Ashkan Daie')).toBeNull();
    expect(screen.getByText('AD')).toBeDefined();
  });

  it('retries when the user switches to a different photo', () => {
    const { rerender } = renderNavUser(USER);
    fireEvent.error(screen.getByAltText('Ashkan Daie'));
    expect(screen.getByText('AD')).toBeDefined();

    // A new photoURL is a new subject: it must get its own load attempt
    // rather than inherit the previous URL's failure.
    rerender(tree({ ...USER, photoURL: `${PHOTO}&v=2` }));
    expect(screen.getByAltText('Ashkan Daie')).toHaveProperty(
      'src',
      `${PHOTO}&v=2`,
    );
  });

  it('renders initials when there is no photo at all', () => {
    renderNavUser({ ...USER, photoURL: null });
    expect(screen.queryByAltText('Ashkan Daie')).toBeNull();
    expect(screen.getByText('AD')).toBeDefined();
  });
});
