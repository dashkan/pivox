// @pivox/features/resource-admin — the descriptor-driven admin abstraction.
// A new admin resource = one `ResourceAdmin` descriptor + a thin per-app route.
// Router- and react-query-agnostic (everything through the injected
// `$api`/`apiClient`), so `apps/start` (SSR/TanStack) and `apps/electron` share it.

export type {
  ListDescriptor,
  ListQueryState,
} from '@/resource-admin/list-descriptor';
export { useResourceList } from '@/resource-admin/use-resource-list';
export type {
  FormDescriptor,
  RecordQueryState,
} from '@/resource-admin/form-descriptor';
export { useResourceForm } from '@/resource-admin/use-resource-form';
export type {
  ResourceFormResult,
  ResourceRemoveState,
} from '@/resource-admin/use-resource-form';
export type {
  ResourceAdmin,
  ResourceViewProps,
} from '@/resource-admin/resource-admin';
export {
  describeCaughtError,
  describeRpcError,
  isFailedPrecondition,
  mapDeleteError,
} from '@/resource-admin/rpc-error';
export type { ApiError } from '@/resource-admin/rpc-error';
