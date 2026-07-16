'use client';

import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@pivox/primitives/combobox';
import { useEffect, useRef, useState } from 'react';

/** A suggestion shown in a creatable combobox: a name plus a one-line hint. */
export interface Suggestion {
  name: string;
  description: string;
}

interface Item {
  value: string;
  label: string;
  description?: string;
}

/** Description shown on the transient "type your own value" option. */
const CUSTOM_DESCRIPTION = 'Custom header';

/**
 * Creatable single-value combobox: pick a suggestion, or type any custom value
 * and commit it. Behaviour (native-select-with-typeahead + creatable):
 *
 * - The DISPLAYED input value and the FILTER query are decoupled. Opening the
 *   field keeps the committed value visible in the input, while a custom
 *   `filter` shows the FULL list (reopening a committed value doesn't narrow to
 *   just that one). Filtering + `autoHighlight` only engage once the user types
 *   something different from the committed value.
 * - A non-matching query surfaces a transient raw-value item (labelled with the
 *   typed text, described as "Custom header") so a custom value is selectable.
 * - Enter / clicking an item commits it; blurring commits the typed text
 *   (never clears — an untouched open restores the committed value).
 *
 * The Base UI Combobox is select-only, hence the transient item to make a custom
 * value selectable. No dialog — inline commit.
 */
export function SuggestCombobox({
  value,
  onChange,
  suggestions,
  placeholder,
  ariaLabel,
  emptyText = 'No matches',
  disabled,
  container,
  collisionBoundary,
}: {
  value: string;
  onChange: (value: string) => void;
  suggestions: Suggestion[];
  placeholder?: string;
  ariaLabel?: string;
  emptyText?: string;
  disabled?: boolean;
  container?: React.RefObject<HTMLElement | null>;
  collisionBoundary?: Element | null;
}) {
  // Displayed input text. Independent of the filter query: it holds the
  // committed value on open and is only replaced as the user types.
  const [inputValue, setInputValue] = useState(value);
  // Resync the display when the committed value changes (a commit, or an
  // external row shift). `value` never changes mid-typing — typing fires only
  // onInputValueChange, not onChange — so this can't clobber keystrokes.
  useEffect(() => {
    setInputValue(value);
  }, [value]);
  // Guards the close handler from double-committing right after a selection.
  const justSelected = useRef(false);

  const base: Item[] = suggestions.map((s) => ({
    value: s.name,
    label: s.name,
    description: s.description,
  }));

  const q = inputValue.trim();
  const modified = q.toLowerCase() !== value.trim().toLowerCase();
  const exact = base.some((item) => item.value.toLowerCase() === q.toLowerCase());
  // Transient custom-value item: only while typing a non-empty, non-matching
  // value distinct from the committed one. Rendered like a suggestion so a typed
  // value doesn't look out of place next to the named headers.
  const items: Item[] =
    modified && q && !exact
      ? [...base, { value: q, label: q, description: CUSTOM_DESCRIPTION }]
      : base;

  return (
    <Combobox
      items={items}
      inputValue={inputValue}
      onInputValueChange={(next: string) => setInputValue(next)}
      onValueChange={(item: Item | null) => {
        justSelected.current = true;
        onChange(item?.value ?? '');
      }}
      onOpenChange={(open: boolean, details: { reason?: string }) => {
        if (open) {
          // Keep the committed value visible; the custom `filter` (below) shows
          // the full list without clearing the display.
          return;
        }
        if (justSelected.current) {
          justSelected.current = false; // selection already committed
          return;
        }
        if (details.reason === 'escape-key' || details.reason === 'cancel-open') {
          setInputValue(value); // cancelled — restore, don't commit
          return;
        }
        const typed = inputValue.trim();
        if (typed && typed !== value.trim()) {
          onChange(typed); // commit a typed custom value on blur
        } else {
          setInputValue(value); // nothing typed — restore the committed display
        }
      }}
      filter={(item: Item, queryStr: string) => {
        const needle = queryStr.trim().toLowerCase();
        // Unmodified (input still equals the committed value) → show the FULL
        // list; filtering/highlight only kick in once the user types something
        // different. (An empty input is a substring of everything, so it also
        // shows all.)
        if (needle === value.trim().toLowerCase()) return true;
        return item.label.toLowerCase().includes(needle);
      }}
      autoHighlight
      disabled={disabled}
    >
      {/* No ✕ clear (commits on select/blur); the KeyValueEditor row's remove
          button clears the pair. w-full fills its 50/50 flex cell. */}
      <ComboboxInput
        className="w-full"
        aria-label={ariaLabel}
        placeholder={placeholder}
        disabled={disabled}
      />
      {/* Widen the POPUP (not the trigger) so names + descriptions read on one line. */}
      <ComboboxContent
        container={container}
        collisionBoundary={collisionBoundary ?? undefined}
        className="min-w-72"
      >
        <ComboboxEmpty>{emptyText}</ComboboxEmpty>
        <ComboboxList>
          {(item: Item) => (
            <ComboboxItem key={item.value} value={item}>
              <div className="flex flex-col">
                <span>{item.label}</span>
                {item.description && (
                  <span className="text-xs text-muted-foreground">
                    {item.description}
                  </span>
                )}
              </div>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
