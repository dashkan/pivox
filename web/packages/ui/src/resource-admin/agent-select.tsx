'use client';

import { parseResourceName } from '@pivox/client';

import { OptionCombobox } from './option-combobox';

import type { ComboOption } from './option-combobox';
import type { AgentOption } from './types';

function agentLeaf(name: string): string {
  return parseResourceName(name).agents ?? name;
}

/**
 * "Run on Agent" picker (combobox with typeahead + clear). Empty value (`''`)
 * runs the connector in the cloud — the resting/placeholder state; a non-empty
 * value is an agent resource name. Clearing returns to cloud.
 */
export function AgentSelect({
  value,
  options,
  onChange,
  disabled,
}: {
  value: string;
  options: AgentOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  // An editing connector's current agent must stay selectable even if it's no
  // longer in the fetched list.
  const merged =
    value && !options.some((option) => option.value === value)
      ? [{ value, label: agentLeaf(value) }, ...options]
      : options;
  const comboOptions: ComboOption[] = merged.map((option) => ({
    value: option.value,
    label: option.label,
  }));

  return (
    <OptionCombobox
      options={comboOptions}
      value={value}
      onChange={onChange}
      emptyValue=""
      placeholder={
        options.length === 0
          ? 'No agents — runs in cloud'
          : 'None (runs in cloud)'
      }
      emptyText="No agents found"
      disabled={disabled}
    />
  );
}
