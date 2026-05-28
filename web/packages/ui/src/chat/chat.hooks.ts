'use client';

import { useChatRuntime } from '@assistant-ui/react-ai-sdk';
import { DefaultChatTransport } from 'ai';
import { useEffect, useMemo, useRef, useState } from 'react';

import { useChatContext } from './chat.context';

import type {
  ChatActions,
  ChatContextValue,
  ChatMeta,
  ChatState,
  GetAuthToken,
  PivoxStreamChatBody,
  PivoxUIMessage,
} from './chat.types';

/**
 * Options for `useChatState` — the public configuration the route
 * layer (or feature wrapper) passes in to construct the Chat context
 * value. Stable identities are NOT required for the function values;
 * `useChatState` uses the latest-ref pattern internally so consumer-
 * supplied callbacks can be inline arrows without churning the
 * transport.
 */
export interface UseChatStateOptions {
  /**
   * The AIP-style parent. Format:
   *   `organizations/{organization}/users/{user}`
   *
   * Changing this value re-creates the underlying transport — the
   * URL changes, so existing in-flight turns are cancelled.
   */
  parent: string;

  /**
   * Optional initial conversation. Omit for a stateless first turn;
   * the server creates the conversation and the resource name surfaces
   * via the chunk stream's `start.messageMetadata`. After that,
   * callers should `actions.setConversation(name)` to attach
   * subsequent turns.
   */
  initialConversation?: string;

  /**
   * Optional initial per-call system instruction. Mutate later via
   * `actions.setSystemInstruction`.
   */
  initialSystemInstruction?: string;

  /**
   * Optional base URL override. Undefined / empty → same-origin
   * (web behind nginx). Electron renderer supplies the remote API
   * origin here.
   */
  baseUrl?: string;

  /**
   * Resolves the bearer token. See `GetAuthToken` for caching guidance.
   */
  getAuthToken: GetAuthToken;
}

/**
 * Builds the Chat context value: assistant-ui runtime wired to the
 * Pivox SSE endpoint, plus Pivox-specific `{state, actions, meta}`.
 *
 * Mirror of `useImageEditorState` — the UI package owns the hook that
 * constructs the context; the features package (or a route) wraps it
 * in `Chat.Provider`.
 *
 * Internally:
 *   - getAuthToken is stashed in a ref so an inline arrow from the
 *     consumer doesn't rebuild the transport every render.
 *   - conversation + systemInstruction live in local state so
 *     `actions.set*` updates trigger a transport rebuild — intentional:
 *     stale closures would smuggle the wrong conversation into
 *     subsequent turns.
 *   - The DefaultChatTransport hits
 *     `POST /v1/${parent}:streamGenerateContent`, the URL the Pivox
 *     SSE handler is registered at. Body fields: messages, plus
 *     optional conversation + systemInstruction. Vercel-only fields
 *     (id, trigger, messageId) pass through; the server's protojson
 *     decoder ignores unknowns via DiscardUnknown.
 */
export function useChatState(opts: UseChatStateOptions): ChatContextValue {
  const {
    parent,
    initialConversation,
    initialSystemInstruction,
    baseUrl,
    getAuthToken,
  } = opts;

  const [conversation, setConversation] = useState<string | undefined>(
    initialConversation,
  );
  const [systemInstruction, setSystemInstruction] = useState<string | undefined>(
    initialSystemInstruction,
  );

  // Latest-ref for the token getter. The headers callback below
  // dereferences this asynchronously at request time — by then the
  // useEffect below has run, so the ref always holds the most recent
  // consumer-supplied getter. Avoids the lint rule against ref
  // writes during render.
  const getAuthTokenRef = useRef<GetAuthToken>(getAuthToken);
  useEffect(() => {
    getAuthTokenRef.current = getAuthToken;
  });

  const transport = useMemo(() => {
    const apiBase = baseUrl ?? '';
    // react-hooks/refs flags the ref deref inside the headers
    // async callback as "during render" because the rule can't see
    // that the callback is deferred — DefaultChatTransport stores
    // it for later, the SDK invokes it at request time. The
    // latest-ref pattern is exactly what useEffect above syncs;
    // disabling the rule here is the documented escape hatch.
    // eslint-disable-next-line react-hooks/refs
    return new DefaultChatTransport<PivoxUIMessage>({
      api: `${apiBase}/v1/${parent}:streamGenerateContent`,
      headers: async () => ({
        Authorization: `Bearer ${await getAuthTokenRef.current()}`,
      }),
      // When `conversation` is set, the server has the full prior
      // history in its DB — re-sending it from the client wastes
      // bandwidth, blows up tokens, and re-opens the "client claims
      // assistant context" trust hole. Send only the latest message
      // (the new user turn, or a tool result). On the very first
      // turn `conversation` is undefined so the full list goes —
      // which is just the one initial user message anyway.
      //
      // Body is typed from @pivox/client's OpenAPI-generated
      // AiChatStreamGenerateContentBody (widened to accept
      // PivoxUIMessage on `messages`), so proto changes that add or
      // rename body fields surface here as compile errors.
      prepareSendMessagesRequest: ({ messages }) => {
        const body: PivoxStreamChatBody = {
          messages: conversation ? messages.slice(-1) : messages,
          conversation,
          systemInstruction,
        };
        return { body };
      },
    });
  }, [baseUrl, parent, conversation, systemInstruction]);

  // onFinish captures the server-emitted conversation resource name
  // on the FIRST turn (where the client supplied no conversation, so
  // the server auto-created one). After that, subsequent turns route
  // to the same conversation via `body.conversation` above.
  //
  // Latest-ref on conversation so the comparison inside onFinish
  // always reads the current value — without it, the closure would
  // capture the initial `conversation` value forever and the guard
  // would re-set state every turn.
  const conversationRef = useRef(conversation);
  useEffect(() => {
    conversationRef.current = conversation;
  });

  const runtime = useChatRuntime<PivoxUIMessage>({
    transport,
    onFinish: ({ message }) => {
      if (conversationRef.current) return;
      // message.metadata is typed PivoxChatMessageMetadata via the
      // UIMessage generic — no inline cast needed.
      const next = message.metadata?.conversation;
      if (next) {
        setConversation(next);
      }
    },
  });

  // Stable action identities — useState setters are React-stable
  // already, so this object only re-creates if React itself swaps
  // them (it doesn't, per the rules of hooks).
  const actions = useMemo<ChatActions>(
    () => ({
      setConversation,
      setSystemInstruction,
    }),
    [],
  );

  const state = useMemo<ChatState>(
    () => ({ parent, conversation, systemInstruction }),
    [parent, conversation, systemInstruction],
  );

  const meta = useMemo<ChatMeta>(() => ({ runtime }), [runtime]);

  return useMemo<ChatContextValue>(
    () => ({ state, actions, meta }),
    [state, actions, meta],
  );
}

/**
 * Selector hook for the conversation field. Avoids re-rendering on
 * unrelated state changes — Rule: `rerender-derived-state`.
 *
 * Provided as a convenience for routes that conditionally render
 * based on whether a conversation is active.
 */
export function useChatConversation(): string | undefined {
  return useChatContext().state.conversation;
}

/**
 * Stable action accessor. Returns the same identity across renders
 * (assuming the Provider above hasn't been replaced), suitable as
 * a dep in effects.
 */
export function useChatActions(): ChatActions {
  return useChatContext().actions;
}

// Re-export the type for callers consuming via `@pivox/ui/chat`.
export type { GetAuthToken };
