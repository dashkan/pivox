// Resource-name → path-param helpers, consumed by the domain hooks and CRUD
// surfaces that call the injected $api/apiClient.
export { isSpaceScoped, resourcePathParams } from '@/workflows/resource-paths';
export type { PathParams } from '@/workflows/resource-paths';
