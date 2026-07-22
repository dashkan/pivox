// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { NavMainItem } from '@pivox/ui/app-shell';

// Router + storage are the two sources the nav derives active scope from.
// Mock both so we can drive the exact (route params, live cookie) combinations.
const paramsMock = vi.fn<() => { organization?: string; space?: string }>(
  () => ({}),
);
vi.mock('@tanstack/react-router', () => ({
  useRouterState: () => '/organizations/acme', // some non-flat path
  useParams: () => paramsMock(),
}));

const activeOrgMock = vi.fn<() => string | null>(() => null);
vi.mock('@pivox/storage/react', () => ({
  useStorageValue: () => activeOrgMock(),
}));

import { useNavMain } from '../src/lib/use-nav-main';

/** A nav sub-item href by (group title, sub-item title). */
function href(items: NavMainItem[], group: string, sub: string): string | undefined {
  return items
    .find((i) => i.title === group)
    ?.items?.find((s) => s.title === sub)?.href;
}

/** Every scope-bearing resource link + its scoped path segment. */
const RESOURCES: Array<[group: string, sub: string, segment: string]> = [
  ['Workflows', 'Connectors', 'connectors'],
  ['Workflows', 'Definitions', 'workflows'],
  ['Admin', 'Secrets', 'secrets'],
];

beforeEach(() => {
  paramsMock.mockReturnValue({});
  activeOrgMock.mockReturnValue(null);
});

describe('useNavMain — resource links resolve to the active org', () => {
  it.each(RESOURCES)(
    'scopes %s > %s to the route $organization param',
    (group, sub, segment) => {
      paramsMock.mockReturnValue({ organization: 'acme' });
      const { result } = renderHook(() => useNavMain(null));
      expect(href(result.current, group, sub)).toBe(
        `/organizations/acme/${segment}`,
      );
    },
  );

  // THE regression class: on a flat route (no param) with a live last-visited
  // cookie, every resource link must scope to that org — NOT /organizations.
  // (Connectors shipped broken here; secrets/workflows must not repeat it.)
  it.each(RESOURCES)(
    'falls %s > %s back to the LIVE last-visited cookie',
    (group, sub, segment) => {
      paramsMock.mockReturnValue({});
      activeOrgMock.mockReturnValue('organizations/globex');
      const { result } = renderHook(() => useNavMain(null)); // frozen seed null
      expect(href(result.current, group, sub)).toBe(
        `/organizations/globex/${segment}`,
      );
    },
  );

  it.each(RESOURCES)(
    'points %s > %s at the selector only when no org is resolvable',
    (group, sub) => {
      const { result } = renderHook(() => useNavMain(null));
      expect(href(result.current, group, sub)).toBe('/organizations');
    },
  );

  // THE bug: inside a space, navigating between areas must KEEP the space —
  // every resource is org-or-space scoped, so a nav link within a space route
  // stays in that space instead of dropping to the org rollup ("All spaces").
  it.each(RESOURCES)(
    'preserves the current space for %s > %s',
    (group, sub, segment) => {
      paramsMock.mockReturnValue({ organization: 'acme', space: 'dev' });
      const { result } = renderHook(() => useNavMain(null));
      expect(href(result.current, group, sub)).toBe(
        `/organizations/acme/spaces/dev/${segment}`,
      );
    },
  );
});
