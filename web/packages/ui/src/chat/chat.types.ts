/**
 * Public types for the Chat composable. Mirrors the `{state, actions,
 * meta}` interface used by ImageEditor and AppShell.
 *
 * Wire-side types are derived from `@pivox/client`'s OpenAPI-generated
 * `components` so a proto reshape automatically flows through here —
 * no parallel TypeScript copy of the proto. The few Pivox-specific
 * Vercel-side types (the chat metadata shape, the UIMessage generic
 * arg) are declared inline because protojson can't generate them from
 * a `google.protobuf.Struct`.
 */

import type { AssistantRuntime } from '@assistant-ui/react';
import type { components } from '@pivox/client';
import type { UIMessage } from 'ai';

/**
 * Resolves the bearer token forwarded to the gRPC AuthInterceptor on
 * every request. Stale tokens surface as 401s the user can recover
 * from by re-auth — assistant-ui retries — so getters should NOT
 * cache aggressively. Web: `firebase.auth().currentUser.getIdToken()`.
 * Electron: IPC to main for the keychain-backed token.
 */
export type GetAuthToken = () => Promise<string>;

/**
 * Reactive Chat state. Subscribers re-render on any field change.
 *
 * Per-message state (the message stream, run/idle, composer text)
 * lives inside assistant-ui's runtime, not here — subcomponents reach
 * it via assistant-ui's own primitives (Thread / Message / Composer).
 * What lives here is Pivox-specific chat configuration that the route
 * layer can mutate.
 */
export interface ChatState {
  /**
   * The AIP-style parent that scopes this chat. Format:
   *   `organizations/{organization}/users/{user}`
   *
   * Embedded in the URL of the SSE endpoint
   * (`POST /v1/${parent}:streamGenerateContent`), so the gRPC
   * permission interceptor gates on `ai.chat.stream` against that org.
   */
  parent: string;

  /**
   * The conversation to attach turns to. When set, the server
   * hydrates history and persists the new turn. Unset → stateless
   * call. Setting this to a new value re-creates the transport.
   */
  conversation: string | undefined;

  /**
   * Optional per-call system instruction. Overrides the conversation's
   * stored system instruction for THIS call only.
   */
  systemInstruction: string | undefined;
}

/**
 * Chat actions. Stable identities — mutating these does not re-render
 * subscribers.
 */
export interface ChatActions {
  /**
   * Switch to a different conversation (or start a stateless call when
   * passed undefined). Re-creates the transport — in-flight turns are
   * cancelled, no message state is preserved across the switch.
   */
  setConversation: (conversation: string | undefined) => void;

  /**
   * Replace the per-call system instruction. Takes effect on the NEXT
   * turn; mid-stream changes don't affect the active generation.
   */
  setSystemInstruction: (systemInstruction: string | undefined) => void;
}

/**
 * Incidental Chat values that don't trigger re-renders. The assistant-ui
 * runtime is the headless engine driving the chat; it's referenced by
 * the Provider when wiring the inner AssistantRuntimeProvider and
 * exposed here so feature-level integrations (e.g. keyboard shortcut
 * to focus the composer) can read it without subscribing.
 */
export interface ChatMeta {
  runtime: AssistantRuntime;
}

export interface ChatContextValue {
  state: ChatState;
  actions: ChatActions;
  meta: ChatMeta;
}

/**
 * Pivox-specific metadata carried on UIMessage.metadata. Currently
 * just the persisted conversation handle the server returns in the
 * first `start` chunk; future fields go here.
 *
 * Not generated from proto because the server-side carrier
 * (`google.protobuf.Struct`) maps to `Record<string, never>` in the
 * OpenAPI output — the schema-free Struct can't surface keys to
 * openapi-typescript.
 */
export interface PivoxChatMessageMetadata {
  /**
   * Resource name of the persisted conversation:
   *   `organizations/{organization}/users/{user}/conversations/{conversation}`
   *
   * Set by the server in the first `start` chunk for first-turn
   * (auto-create) calls. Captured by `useChatState`'s `onFinish`
   * and threaded back via `body.conversation` on subsequent turns.
   */
  conversation?: string;
}

/**
 * The UIMessage flavor the chat runtime speaks: Vercel UIMessage
 * parameterized with the Pivox metadata shape so `message.metadata`
 * reads as `PivoxChatMessageMetadata` instead of `unknown`.
 */
export type PivoxUIMessage = UIMessage<PivoxChatMessageMetadata>;

/**
 * Body the chat transport POSTs to
 * `/v1/{parent}:streamGenerateContent`. Derived from
 * `@pivox/client`'s OpenAPI `AiChatStreamGenerateContentBody` so a
 * proto reshape (new fields, renames) propagates automatically; the
 * `messages` field is widened from `v1InputMessage[]` to
 * `PivoxUIMessage[]` because the runtime works in Vercel-shape and
 * the two are wire-compatible after the MessagePart proto reshape.
 */
export type PivoxStreamChatBody = Omit<
  components['schemas']['AiChatStreamGenerateContentBody'],
  'messages'
> & {
  messages: PivoxUIMessage[];
};
