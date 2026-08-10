'use client';

import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@pivox/primitives/combobox';

/** A selectable combobox option: `value` is stored, `label` is shown + searched. */
export interface ComboOption {
  value: string;
  label: string;
}

/**
 * Single-select combobox over string-valued options, with typeahead + clear.
 * `emptyValue` is the "no selection" value: when `value === emptyValue` nothing
 * is selected (the placeholder shows), and clearing emits `emptyValue`. Callers
 * keep a plain `value: string` / `onChange(value)` contract — the Base UI
 * object-item wiring stays inside here.
 *
 * The popup portals to `<body>`. It previously took `container` /
 * `collisionBoundary` to escape Radix Dialog's body-level pointer-lock; Base UI
 * scopes modality to an internal backdrop instead, so that plumbing is gone.
 */
export function OptionCombobox({
  options,
  value,
  onChange,
  emptyValue,
  placeholder,
  emptyText,
  disabled,
}: {
  options: ComboOption[];
  value: string;
  onChange: (value: string) => void;
  emptyValue: string;
  placeholder?: string;
  emptyText: string;
  disabled?: boolean;
}) {
  const selected =
    value === emptyValue
      ? null
      : (options.find((option) => option.value === value) ?? null);

  return (
    <Combobox
      items={options}
      value={selected}
      onValueChange={(item: ComboOption | null) =>
        onChange(item?.value ?? emptyValue)
      }
      // Auto-highlight the first match while typing so Enter selects it.
      autoHighlight
      disabled={disabled}
    >
      <ComboboxInput placeholder={placeholder} showClear disabled={disabled} />
      <ComboboxContent>
        <ComboboxEmpty>{emptyText}</ComboboxEmpty>
        <ComboboxList>
          {(item: ComboOption) => (
            <ComboboxItem key={item.value} value={item}>
              {item.label}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
