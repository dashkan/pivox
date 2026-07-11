/**
 * User-profile metadata types, vendored from Keycloak's
 * `@keycloak/keycloak-account-ui` (`lib/api/representations.d.ts`). Structure
 * is identical; the two index signatures are tightened from upstream's `any`
 * to `unknown` (call sites already narrow before use).
 *
 * We only ever consumed these two type declarations from that package — the
 * account-ui *logic* is already reimplemented against Pivox primitives (see
 * `./utils.ts`). Keeping the dep just for two types dragged in an older
 * i18next/react-i18next (peer `typescript: ^5 || ^6`) transitively, which
 * left an unmet-peer warning under our TS7. Vendoring the types lets us drop
 * `@keycloak/keycloak-account-ui` + `@keycloak/keycloak-ui-shared` entirely.
 *
 * Shape must match what Keycloak's account REST endpoint actually returns
 * (`/?userProfileMetadata=true`) — keep in sync with upstream if the KC
 * server version changes.
 */

export interface UserProfileAttributeMetadata {
  name: string;
  displayName: string;
  required: boolean;
  readOnly: boolean;
  annotations?: {
    [index: string]: unknown;
  };
  validators: {
    [index: string]: {
      [index: string]: unknown;
    };
  };
  multivalued: boolean;
  defaultValue: string;
}

export interface UserProfileMetadata {
  attributes: UserProfileAttributeMetadata[];
}
