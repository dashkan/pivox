'use client';

import { useChatState } from '@pivox/ui/chat';

import type { ChatContextValue, UseChatStateOptions } from '@pivox/ui/chat';

/**
 * Feature-level options for ChatFeature. Mirror of
 * `UseImageEditorFeatureOptions` — for now a pure pass-through to the
 * UI hook, with the option to layer feature-only concerns later
 * (keyboard shortcuts to focus the composer, IPC for share-to-system,
 * analytics, etc.). The split keeps wiring out of UI and rendering
 * out of features.
 */
export type UseChatFeatureOptions = UseChatStateOptions;

/**
 * Feature-level hook. Today: a thin pass-through to `useChatState`.
 * As feature-specific wiring is added (kbd shortcuts, IPC, etc.) it
 * lives here without polluting the UI hook.
 */
export function useChatFeature(opts: UseChatFeatureOptions): ChatContextValue {
  return useChatState(opts);
}
