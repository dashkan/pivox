'use client';

import { OptionCombobox } from './option-combobox';

import type { ComboOption } from './option-combobox';
import type { AgentOption } from './types';

// "No predicate" (Any) vs "agent is unset" (Cloud) are distinct filter meanings,
// each needing a sentinel separate from any real agent resource name. Any is the
// empty/default state (no selection); Cloud + the in-scope agents are options.
export const AGENT_FILTER_ANY = '__any__';
export const AGENT_FILTER_CLOUD = '__cloud__';

/**
 * Agent predicate picker for a list filter row (combobox with typeahead + clear).
 * "Any" applies no predicate (the resting/default state), "Cloud" matches
 * connectors with no agent (empty `agent`), and each option matches its exact
 * agent resource name. Clearing returns to "Any".
 */
export function AgentFilterSelect({
  value,
  options,
  onChange,
}: {
  value: string;
  options: AgentOption[];
  onChange: (value: string) => void;
}) {
  const comboOptions: ComboOption[] = [
    { value: AGENT_FILTER_CLOUD, label: 'Cloud' },
    ...options.map((option) => ({ value: option.value, label: option.label })),
  ];

  return (
    <OptionCombobox
      options={comboOptions}
      value={value}
      onChange={onChange}
      emptyValue={AGENT_FILTER_ANY}
      placeholder="Any agent"
      emptyText="No agents found"
    />
  );
}
