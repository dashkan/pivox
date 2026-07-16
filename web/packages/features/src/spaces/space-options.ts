import { parseResourceName } from '@pivox/client';

import type { components } from '@pivox/client/types';
import type { SpaceOption } from '@pivox/ui/resource-admin';

type Space = components['schemas']['v1Space'];

/**
 * Flatten spaces into scope-dropdown options: slug = the resource-name leaf (the
 * `{space}` path param), label = the display name (or slug when unset). Spaces
 * without a name are dropped — the name is the selectable identity.
 */
export function toSpaceOptions(spaces: Space[]): SpaceOption[] {
  const out: SpaceOption[] = [];
  for (const space of spaces) {
    if (!space.name) continue;
    const slug = parseResourceName(space.name).spaces ?? '';
    if (!slug) continue;
    out.push({ name: space.name, slug, displayName: space.displayName || slug });
  }
  return out;
}
