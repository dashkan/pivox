import * as React from 'react';

/**
 * Shape an org needs to be displayable in the scope picker. Maps to the
 * subset of fields the AccountOrganization proto we read from
 * `/v1/accounts/me/organizations` actually carries.
 *
 * `logo` is optional — providers leave it unset until the AIP org
 * resource ships a logo field, and the picker derives a fallback
 * (first two letters of displayName) for missing values.
 *
 * The old sidebar org-only picker (`AppShellOrgPicker`) was replaced by
 * `AppShellScopePicker` once scope moved into the URL; this interface
 * survives as the shared org shape both the picker and feature hook use.
 */
export interface OrgPickerOrg {
  /** Resource name, e.g. "organizations/acme". Used as the stable id. */
  organization: string;
  displayName: string;
  logo?: React.ReactNode;
}
