/**
 * Test-only utilities for `@pivox/storage`. Imported via the
 * `@pivox/storage/test-utils` subpath so production code can't reach
 * for `__resetRegistryForTests` without going out of its way.
 *
 * Keeping this separate from the root entry means:
 *   - Tree-shakers can drop the entire test-utils module from prod
 *     bundles even if a consumer accidentally types the import.
 *   - The root `@pivox/storage` autocomplete surface stays clean —
 *     consumers don't see `__resetRegistryForTests` and don't think
 *     it's part of the supported API.
 */
export { __resetRegistryForTests } from './define';
export { __resetChannelForTests } from './notify';
