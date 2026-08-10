'use client';

import { OptionCombobox } from './option-combobox';

import type { ComboOption } from './option-combobox';
import type { SpaceOption } from './types';

/**
 * Space scope picker (combobox with typeahead + clear). The empty value ('')
 * means no space — a non-empty value is a space slug. When empty the trigger
 * shows `placeholder` if given (the create form's "No space — organization",
 * reading as unset) otherwise `allLabel` (the filter's "All spaces"). Clearing
 * returns to the empty value (org-direct / all-spaces).
 */
export function ScopeSelect({
  value,
  spaces,
  onChange,
  allLabel,
  placeholder,
  disabled,
}: {
  value: string;
  spaces: SpaceOption[];
  onChange: (value: string) => void;
  allLabel: string;
  placeholder?: string;
  disabled?: boolean;
  /** Portal target for the popup; pass the dialog's content when used in a modal. */
  /** Flip/shift boundary; pass the dialog element when used in a modal. */
}) {
  const options: ComboOption[] = spaces.map((space) => ({
    value: space.slug,
    label: space.displayName || space.slug,
  }));

  return (
    <OptionCombobox
      options={options}
      value={value}
      onChange={onChange}
      emptyValue=""
      placeholder={placeholder ?? allLabel}
      emptyText="No spaces found"
      disabled={disabled}
    />
  );
}
