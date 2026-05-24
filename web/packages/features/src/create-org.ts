/**
 * `@pivox/features/create-org` — the post-sign-in onboarding screen
 * for users with zero organizations. Backed by the synchronously-
 * completed `CreateOrganization` REST call; the org gate re-fetches
 * on success and routes the user into the app.
 *
 * Mirrors the SwiftUI native flow at
 * `native/platform/macos/swift/Auth/CreateOrgView.swift`.
 */

export { CreateOrgFeature } from './create-org/create-org-feature';
export { useCreateOrg } from './create-org/use-create-org';
export { isValidSlug, slugify } from './create-org/slug';
