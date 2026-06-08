'use client';

import {
  AssistantRuntimeProvider,
  AuiIf,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
} from '@assistant-ui/react';
import { Avatar, AvatarFallback } from '@pivox/primitives/avatar';
import { Button } from '@pivox/primitives/button';
import { Textarea } from '@pivox/primitives/textarea';
import { cn } from '@pivox/primitives/utils';
import { SendHorizonalIcon, SquareIcon } from 'lucide-react';

import { ChatContext } from './chat.context';

import type { ChatContextValue } from './chat.types';
import type { ComponentProps, FC, ReactNode } from 'react';

/**
 * Chat is the Pivox chat surface — three high-level atoms over
 * assistant-ui's headless runtime primitives, styled with
 * `@pivox/primitives`. Mirrors the `ImageEditor` / `AppShell`
 * compound-component shape but at a coarser grain.
 *
 * Decomposition lives at the REGION level (header / thread /
 * composer), not at the primitive level (button / input / message).
 * Each region has a clear ownership of internal layout, so the
 * redesign can swap visuals inside `Chat.Thread` or `Chat.Input`
 * without churning the consumer's compose call.
 *
 * Usage:
 *
 *   <ChatFeature parent={...} getAuthToken={...}>
 *     <Chat.Header>
 *       <MyConversationTitle />
 *       <MyNewChatButton />
 *     </Chat.Header>
 *     <Chat.Thread />
 *     <Chat.Input />
 *   </ChatFeature>
 *
 * For chat surfaces that don't want a header (e.g. inline / embedded
 * chats), omit `Chat.Header` — the layout collapses. The runtime
 * (assistant-ui) and the Pivox-side `{state, actions, meta}` context
 * are provided by `Chat.Provider`, wrapped by `ChatFeature`.
 */

/* ─── Provider ────────────────────────────────────────────────── */

function ChatProvider({
  value,
  children,
}: {
  value: ChatContextValue;
  children: ReactNode;
}) {
  // `flex-1 min-h-0` (not `h-full`) so we compose into a flex parent
  // — e.g. shadcn `SidebarInset` — without colliding with a sibling
  // header. `min-h-0` is the well-known fix that lets the inner
  // viewport scroll instead of pushing the layout past the parent.
  return (
    <ChatContext value={value}>
      <AssistantRuntimeProvider runtime={value.meta.runtime}>
        <ThreadPrimitive.Root className="flex min-h-0 flex-1 flex-col bg-background">
          {children}
        </ThreadPrimitive.Root>
      </AssistantRuntimeProvider>
    </ChatContext>
  );
}

/* ─── Header ──────────────────────────────────────────────────── */

/**
 * Top bar slot. Renders nothing on its own; consumers fill it with
 * whatever the redesign requires (conversation title, model picker,
 * share button, etc.). Defaults to a single horizontal flex row with
 * gap-2; consumers override via className.
 *
 * Place as the FIRST child of `Chat.Provider` — the column-flex
 * layout positions Header on top, Thread fills, Input pinned at
 * bottom, in source order.
 */
const Header: FC<ComponentProps<'div'>> = ({
  className,
  children,
  ...props
}) => (
  <div
    className={cn(
      'flex shrink-0 items-center gap-2 border-b border-border bg-background px-4 py-2',
      className,
    )}
    {...props}
  >
    {children}
  </div>
);

/* ─── Thread ──────────────────────────────────────────────────── */

const DEFAULT_EMPTY_STATE = (
  <p className="text-sm text-muted-foreground">Start the conversation…</p>
);

// Hoisted so ThreadPrimitive.Messages receives a stable function ref
// across Thread re-renders. The render fn closes over nothing
// per-render; defining it inline would allocate every commit.
const renderMessage = ({ message }: { message: { role: string } }) =>
  message.role === 'user' ? <UserMessage /> : <AssistantMessage />;

const UserMessage: FC = () => (
  <MessagePrimitive.Root className="flex w-full justify-end">
    <div
      className={cn(
        'max-w-[80%] rounded-2xl rounded-br-md bg-primary px-4 py-2 text-sm',
        'text-primary-foreground shadow-sm',
      )}
    >
      <MessagePrimitive.Parts />
    </div>
  </MessagePrimitive.Root>
);

const AssistantMessage: FC = () => (
  <MessagePrimitive.Root className="flex w-full gap-3">
    <Avatar className="h-8 w-8 shrink-0">
      <AvatarFallback className="bg-muted text-xs font-medium">
        AI
      </AvatarFallback>
    </Avatar>
    <div
      className={cn(
        'max-w-[80%] rounded-2xl rounded-tl-md bg-muted px-4 py-2 text-sm',
        'text-foreground shadow-sm',
      )}
    >
      <MessagePrimitive.Parts />
    </div>
  </MessagePrimitive.Root>
);

type ThreadProps = {
  /**
   * Override the empty-state content. Defaults to a muted
   * "Start the conversation…" line.
   */
  empty?: ReactNode;
} & Omit<ComponentProps<typeof ThreadPrimitive.Viewport>, 'children'>;

/**
 * Scrolling message list with empty state. Renders the default
 * user/assistant message bubbles internally — consumers wanting a
 * different message shape should compose `MessagePrimitive` + the
 * `useMessage()` hook directly (a third-layer Chat namespace
 * abstraction would just be visual noise).
 */
const Thread: FC<ThreadProps> = ({ empty, className, ...props }) => (
  <ThreadPrimitive.Viewport
    className={cn(
      'flex flex-1 flex-col overflow-y-auto scroll-smooth bg-background px-4 py-6',
      className,
    )}
    {...props}
  >
    <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col">
      <AuiIf condition={(s) => s.thread.isEmpty}>
        <div className="flex flex-1 flex-col items-center justify-center text-center">
          {empty ?? DEFAULT_EMPTY_STATE}
        </div>
      </AuiIf>
      {/* `mt-auto` absorbs the slack above the messages so they pin to
          the bottom when the thread underfills the viewport. Collapses
          to 0 once content overflows, so scroll-up stays reachable —
          unlike `justify-end`, which clips the overflowing top. */}
      <div className="mt-auto flex flex-col gap-6">
        <ThreadPrimitive.Messages>{renderMessage}</ThreadPrimitive.Messages>
      </div>
    </div>
  </ThreadPrimitive.Viewport>
);

/* ─── Input ───────────────────────────────────────────────────── */

/**
 * Bottom composer bar. Hard-codes the textarea + Send/Cancel button
 * swap because there's no legitimate Pivox-side variation today; if
 * we ever need a different composer shape (attachments toolbar,
 * voice input toggle), the redesign rebuilds this from
 * `ComposerPrimitive` directly. The atoms (ComposerForm,
 * ComposerInput, ComposerSend) were public in the previous Chat
 * namespace and have been removed — they were unused and exposed
 * inner primitives the consumer shouldn't reach.
 */
const Input: FC<ComponentProps<typeof ComposerPrimitive.Root>> = ({
  className,
  ...props
}) => (
  <ComposerPrimitive.Root
    className={cn(
      'flex w-full items-end gap-2 border-t border-border bg-background p-3',
      className,
    )}
    {...props}
  >
    <div className="mx-auto flex w-full max-w-3xl items-end gap-2">
      {/* jsx-a11y/no-autofocus: omitted — auto-focusing a textarea
          on mount hijacks keyboard context for assistive tech. */}
      <ComposerPrimitive.Input asChild>
        <Textarea
          rows={1}
          placeholder="Send a message…"
          className="min-h-[44px] resize-none"
        />
      </ComposerPrimitive.Input>
      <AuiIf condition={(s) => !s.thread.isRunning}>
        <ComposerPrimitive.Send asChild>
          <Button
            type="submit"
            size="icon"
            aria-label="Send message"
            className="shrink-0"
          >
            <SendHorizonalIcon className="size-4" />
          </Button>
        </ComposerPrimitive.Send>
      </AuiIf>
      <AuiIf condition={(s) => s.thread.isRunning}>
        <ComposerPrimitive.Cancel asChild>
          <Button
            type="button"
            size="icon"
            variant="secondary"
            aria-label="Stop generation"
            className="shrink-0"
          >
            <SquareIcon className="size-4" />
          </Button>
        </ComposerPrimitive.Cancel>
      </AuiIf>
    </div>
  </ComposerPrimitive.Root>
);

/* ─── Namespace export ───────────────────────────────────────── */

export const Chat = {
  Provider: ChatProvider,
  Header,
  Thread,
  Input,
  Context: ChatContext,
};
