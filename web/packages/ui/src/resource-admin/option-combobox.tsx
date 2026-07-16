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
 * `container` portals the popup into a specific element (a ref). Needed inside a
 * modal dialog: the dialog's pointer-lock disables clicks outside its content,
 * so the default body portal is unclickable by mouse — pass the dialog's own
 * content/form element. Undefined (the default) portals to `<body>`.
 *
 * `collisionBoundary` sets the box the popup flips/shifts within (default: the
 * viewport). In a dialog, pass the dialog/form element so a bottom-of-dialog
 * popup flips ABOVE the input instead of overrunning the dialog footer.
 */
export function OptionCombobox({
  options,
  value,
  onChange,
  emptyValue,
  placeholder,
  emptyText,
  disabled,
  container,
  collisionBoundary,
}: {
  options: ComboOption[];
  value: string;
  onChange: (value: string) => void;
  emptyValue: string;
  placeholder?: string;
  emptyText: string;
  disabled?: boolean;
  container?: React.RefObject<HTMLElement | null>;
  collisionBoundary?: Element | null;
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
      <ComboboxContent
        container={container}
        collisionBoundary={collisionBoundary ?? undefined}
      >
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
