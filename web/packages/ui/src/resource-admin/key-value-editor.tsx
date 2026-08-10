'use client';

import { Button } from '@pivox/primitives/button';
import { FieldDescription, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import { PlusIcon, XIcon } from 'lucide-react';
import { useRef } from 'react';

import { SuggestCombobox } from './suggest-combobox';

import type { Suggestion } from './suggest-combobox';
import type { KeyValueEntry } from './types';

/**
 * Controlled key/value map editor (connector headers, annotations). A trailing
 * empty row is always shown so a new pair can be added without a separate
 * button press; `onChange` reports the full row list and the caller drops
 * blank-key rows when serializing.
 *
 * When `keySuggestions` is given, the KEY field becomes a creatable combobox
 * (common names + descriptions, still freeform); otherwise it stays a plain
 * input, so other callers are unaffected.
 */
export function KeyValueEditor({
  label,
  description,
  keyPlaceholder,
  valuePlaceholder,
  entries,
  onChange,
  disabled,
  keySuggestions,
}: {
  label: string;
  description?: React.ReactNode;
  keyPlaceholder: string;
  valuePlaceholder: string;
  entries: KeyValueEntry[];
  onChange: (entries: KeyValueEntry[]) => void;
  disabled?: boolean;
  keySuggestions?: Suggestion[];
}) {
  const rows: KeyValueEntry[] = [...entries, { key: '', value: '' }];

  // Stable per-row React keys. `entries` carries no id, so a plain `key={index}`
  // shifts every row below a mid-list removal — jumping focus and briefly
  // showing a stale SuggestCombobox draft. Grow by appending (new + sentinel
  // rows land at the end, leaving existing rows' keys untouched, so typing in
  // the sentinel keeps focus); removal splices the removed key so survivors keep
  // theirs; an external wholesale replace resyncs by truncating.
  const rowKeys = useRef<string[]>([]);
  const keySeq = useRef(0);
  while (rowKeys.current.length < rows.length) {
    rowKeys.current.push(`kv-${keySeq.current++}`);
  }
  if (rowKeys.current.length > rows.length) {
    rowKeys.current.length = rows.length;
  }

  const update = (index: number, patch: Partial<KeyValueEntry>) => {
    const next = rows.map((row, i) =>
      i === index ? { ...row, ...patch } : row,
    );
    // Trim the trailing blank sentinel so state holds only real rows.
    onChange(
      next.filter((row, i) => i < next.length - 1 || row.key || row.value),
    );
  };

  const removeRow = (index: number) => {
    rowKeys.current.splice(index, 1); // keep keys aligned so survivors stay put
    onChange(entries.filter((_, i) => i !== index));
  };

  return (
    <div className="flex flex-col gap-2">
      <FieldLabel>{label}</FieldLabel>
      {description && <FieldDescription>{description}</FieldDescription>}
      <div className="flex flex-col gap-2">
        {rows.map((row, index) => {
          const isSentinel = index === rows.length - 1;
          return (
            <div
              key={rowKeys.current[index] ?? `kv-${index}`}
              className="flex items-center gap-2"
            >
              {/* flex-1 + min-w-0 on both fields → true 50/50 that can shrink;
                  the button is shrink-0. (Arbitrary `grid-cols-[...]` didn't
                  compile, collapsing everything into one column.) */}
              <div className="min-w-0 flex-1">
                {keySuggestions ? (
                  <SuggestCombobox
                    ariaLabel={`${label} name ${index + 1}`}
                    placeholder={keyPlaceholder}
                    value={row.key}
                    onChange={(key) => update(index, { key })}
                    suggestions={keySuggestions}
                    disabled={disabled}
                  />
                ) : (
                  <Input
                    aria-label={`${label} name ${index + 1}`}
                    placeholder={keyPlaceholder}
                    value={row.key}
                    onChange={(e) => update(index, { key: e.target.value })}
                    disabled={disabled}
                  />
                )}
              </div>
              <div className="min-w-0 flex-1">
                <Input
                  aria-label={`${label} value ${index + 1}`}
                  placeholder={valuePlaceholder}
                  value={row.value}
                  onChange={(e) => update(index, { value: e.target.value })}
                  disabled={disabled}
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={
                  isSentinel
                    ? `Add ${label} row`
                    : `Remove ${label} row ${index + 1}`
                }
                onClick={() => {
                  if (!isSentinel) removeRow(index);
                }}
                disabled={disabled || isSentinel}
                className={isSentinel ? 'shrink-0 opacity-40' : 'shrink-0'}
              >
                {isSentinel ? <PlusIcon /> : <XIcon />}
              </Button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
