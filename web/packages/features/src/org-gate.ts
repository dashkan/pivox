/**
 * `@pivox/features/org-gate` — post-sign-in gate that ensures the
 * user has at least one organization before the authenticated app
 * shell renders. Zero orgs routes the user to the create-org screen.
 *
 * Mirrors the SwiftUI native `OrgService.bootstrap()` flow at
 * `native/platform/macos/swift/Auth/OrgDirectory.swift`.
 */

export { OrgGateFeature } from './org-gate/org-gate-feature';
export type { NavigateComponent } from './navigation';
export { useOrgGate } from './org-gate/use-org-gate';
export type {
  OrgGateActions,
  OrgGateState,
  OrgGateStatus,
} from './org-gate/use-org-gate';
