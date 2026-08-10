// @vitest-environment jsdom
import { SidebarProvider } from '@pivox/primitives/sidebar';
import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { AppShellScopePicker } from '../../src/app-shell/scope-picker';

import type { ScopePickerProps } from '../../src/app-shell/scope-picker';

// Radix DropdownMenu + SidebarProvider (useIsMobile) touch DOM APIs jsdom lacks.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
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

// jsdom can't resolve computed pointer-events, which user-event asserts on.
const user = userEvent.setup({ pointerEventsCheck: 0 });

const ACME = 'organizations/acme';
const GLOBEX = 'organizations/globex';
const DEV = 'organizations/acme/spaces/dev';
const PROD = 'organizations/acme/spaces/prod';

function makeProps(overrides: Partial<ScopePickerProps> = {}): ScopePickerProps {
  return {
    orgs: [
      { organization: ACME, displayName: 'Acme Inc' },
      { organization: GLOBEX, displayName: 'Globex' },
    ],
    activeOrganization: ACME,
    spaces: [
      { space: DEV, displayName: 'Development', href: '#' },
      { space: PROD, displayName: 'Production', href: '#' },
    ],
    activeSpace: null,
    onSelectOrganization: vi.fn(),
    onSelectSpace: vi.fn(),
    ...overrides,
  };
}

function renderPicker(props: ScopePickerProps) {
  return render(
    <SidebarProvider>
      <AppShellScopePicker {...props} />
    </SidebarProvider>,
  );
}

/**
 * Open the menu and scope queries to it. Base UI's menu reacts to the full
 * pointer/focus sequence, so these drive it through user-event rather than
 * synthesising individual events.
 */
async function openMenu() {
  fireEvent.click(screen.getByRole('button', { name: /acme inc/i }));
  return within(screen.getByRole('menu'));
}

/** Activate a menu item (handler lives on the menuitem, not the text node). */
async function pick(el: HTMLElement) {
  await user.click(el.closest<HTMLElement>('[role="menuitem"]') ?? el);
}

describe('AppShellScopePicker', () => {
  it('shows the active org and the scope label (All spaces when no space)', async () => {
    renderPicker(makeProps());
    const trigger = screen.getByRole('button', { name: /acme inc/i });
    expect(within(trigger).getByText('Acme Inc')).toBeDefined();
    expect(within(trigger).getByText('All spaces')).toBeDefined();
  });

  it('shows the active space name in the trigger when a space is selected', async () => {
    renderPicker(makeProps({ activeSpace: PROD }));
    const trigger = screen.getByRole('button', { name: /acme inc/i });
    expect(within(trigger).getByText('Production')).toBeDefined();
  });

  it('always renders even for a single-org caller', async () => {
    renderPicker(
      makeProps({ orgs: [{ organization: ACME, displayName: 'Acme Inc' }] }),
    );
    expect(
      screen.getByRole('button', { name: /acme inc/i }),
    ).toBeDefined();
  });

  it('lists every org and selects one on click', async () => {
    const onSelectOrganization = vi.fn();
    renderPicker(makeProps({ onSelectOrganization }));
    const menu = await openMenu();
    await pick(menu.getByText('Globex'));
    expect(onSelectOrganization).toHaveBeenCalledWith(GLOBEX);
  });

  it('lists spaces plus "All spaces"; selecting a space passes its name', async () => {
    const onSelectSpace = vi.fn();
    renderPicker(makeProps({ onSelectSpace }));
    const menu = await openMenu();
    await pick(menu.getByText('Development'));
    expect(onSelectSpace).toHaveBeenCalledWith(DEV);
  });

  it('selecting "All spaces" passes null (org rollup)', async () => {
    const onSelectSpace = vi.fn();
    renderPicker(makeProps({ activeSpace: PROD, onSelectSpace }));
    const menu = await openMenu();
    await pick(menu.getByText('All spaces'));
    expect(onSelectSpace).toHaveBeenCalledWith(null);
  });

  it('does not offer a create-organization CTA (members cannot create orgs)', async () => {
    renderPicker(makeProps());
    const menu = await openMenu();
    expect(menu.queryByText('Create Organization')).toBeNull();
  });

  // Regression: the Spaces section must ALWAYS render. Every admin resource
  // (connectors, secrets, workflows) is org-OR-space scoped, so the picker never
  // hides spaces per-resource. Workflows were wrongly treated as org-direct and
  // this section got suppressed on them — this pins that it can't recur.
  it('always renders the Spaces section (no per-resource suppression)', async () => {
    renderPicker(makeProps());
    const menu = await openMenu();
    expect(menu.getByText('Spaces')).toBeDefined();
    expect(menu.getByText('All spaces')).toBeDefined();
    expect(menu.getByText('Development')).toBeDefined();
    expect(menu.getByText('Production')).toBeDefined();
  });

  // Pick org THEN space without reopening: selecting a scope keeps the menu open.
  it('stays open after selecting an org', async () => {
    renderPicker(makeProps());
    const menu = await openMenu();
    await pick(menu.getByText('Globex'));
    // Still open — the Spaces section is reachable in the same session.
    expect(within(screen.getByRole('menu')).getByText('Development')).toBeDefined();
  });

  // Space (and "All spaces") is the terminal selection — auto-dismiss on it.
  it('dismisses after selecting a space', async () => {
    renderPicker(makeProps());
    const menu = await openMenu();
    await pick(menu.getByText('Development'));
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('dismisses after selecting All spaces', async () => {
    renderPicker(makeProps({ activeSpace: PROD }));
    const menu = await openMenu();
    await pick(menu.getByText('All spaces'));
    expect(screen.queryByRole('menu')).toBeNull();
  });
});
