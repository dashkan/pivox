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
 * Chat is the Pivox chat surface, mirroring the `ImageEditor`
 * namespace: a thin Pivox-styled layer over assistant-ui's headless
 * runtime primitives. Each sub-component is a separable atom — the
 * route composes them just like an image editor route composes
 * `<ImageEditor.Toolbar>` + `<ImageEditor.CropButton>` etc.
 *
 * Wiring lives in `@pivox/features/chat` (ChatFeature). The feature
 * owns runtime construction via useChatRuntime and provides the
 * AssistantRuntimeProvider; this namespace only renders against the
 * runtime that provider exposes.
 *
 * Default composition:
 *
 *   <ChatFeature parent={...} getAuthToken={...}>
 *     <Chat.Root>
 *       <Chat.Viewport>
 *         <Chat.Empty>Start the conversation…</Chat.Empty>
 *         <Chat.Messages />
 *       </Chat.Viewport>
 *       <Chat.Composer />
 *     </Chat.Root>
 *   </ChatFeature>
 *
 * Custom layouts can drop the compound `Chat.Composer` and build
 * their own from the atoms (Chat.ComposerForm, Chat.ComposerInput,
 * Chat.ComposerSend, Chat.ComposerCancel, Chat.IfRunning,
 * Chat.IfNotRunning). Consumers can also override the default
 * UserMessage / AssistantMessage by passing a `components` prop to
 * Chat.Messages.
 */

/* ─── Provider ────────────────────────────────────────────────── */

/**
 * Chat.Provider is the namespace's entry point — mirror of
 * `ImageEditor.Provider`. Takes a `value` built by `useChatState`
 * (in `chat.hooks.ts`) and provides BOTH the Pivox `ChatContext`
 * (for chat-level state/actions/meta) AND assistant-ui's
 * `AssistantRuntimeProvider` (driving Thread / Message / Composer
 * primitives downstream).
 *
 * Two providers, one component, because every Chat subcomponent
 * needs both layers — splitting them at the consumer site would
 * invite mismatched mounts where one provider exists without the
 * other.
 *
 * Usage at the feature layer:
 *
 *   const value = useChatState({ parent, getAuthToken, ... });
 *   return <Chat.Provider value={value}>{children}</Chat.Provider>;
 */
function ChatProvider({
  value,
  children,
}: {
  value: ChatContextValue;
  children: ReactNode;
}) {
  return (
    <ChatContext value={value}>
      <AssistantRuntimeProvider runtime={value.meta.runtime}>
        {children}
      </AssistantRuntimeProvider>
    </ChatContext>
  );
}

/* ─── Container atoms ─────────────────────────────────────────── */

type RootProps = ComponentProps<typeof ThreadPrimitive.Root>;

const Root: FC<RootProps> = ({ className, ...props }) => (
  <ThreadPrimitive.Root
    className={cn('flex h-full flex-col bg-background', className)}
    {...props}
  />
);

type ViewportProps = ComponentProps<typeof ThreadPrimitive.Viewport>;

const Viewport: FC<ViewportProps> = ({ className, children, ...props }) => (
  <ThreadPrimitive.Viewport
    className={cn(
      'flex flex-1 flex-col overflow-y-auto scroll-smooth bg-background px-4 py-6',
      className,
    )}
    {...props}
  >
    <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-6">
      {children}
    </div>
  </ThreadPrimitive.Viewport>
);

// v0.14: ThreadPrimitive.Empty is deprecated in favor of AuiIf with a
// state selector. AuiIf unmounts children when condition flips, so
// the empty state vanishes the moment the first message arrives —
// same semantics, more general.
const Empty: FC<ComponentProps<'div'>> = ({ className, children, ...props }) => (
  <AuiIf condition={(s) => s.thread.isEmpty}>
    <div
      className={cn(
        'flex flex-1 flex-col items-center justify-center text-center',
        'text-muted-foreground',
        className,
      )}
      {...props}
    >
      {children}
    </div>
  </AuiIf>
);

/* ─── Message renderers (the components prop targets) ─────────── */

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
      <AvatarFallback className="bg-muted text-xs font-medium">AI</AvatarFallback>
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

/* ─── Messages list ──────────────────────────────────────────── */

type MessageRoleComponent = FC;

type MessagesProps = {
  /**
   * Override the user-role renderer. Defaults to `Chat.UserMessage`.
   */
  userMessage?: MessageRoleComponent;
  /**
   * Override the assistant-role renderer. Defaults to
   * `Chat.AssistantMessage`.
   */
  assistantMessage?: MessageRoleComponent;
};

// v0.14: ThreadPrimitive.Messages migrated from a `components` prop
// to a children render function — `<Messages>{({message}) => ...}</>`.
// The render function fires per message; branch on message.role to
// pick the renderer.
const Messages: FC<MessagesProps> = ({
  userMessage: UserMsg = UserMessage,
  assistantMessage: AssistantMsg = AssistantMessage,
}) => (
  <ThreadPrimitive.Messages>
    {({ message }) =>
      message.role === 'user' ? <UserMsg /> : <AssistantMsg />
    }
  </ThreadPrimitive.Messages>
);

/* ─── Composer atoms ─────────────────────────────────────────── */

/**
 * ComposerForm is the `<form>` root. Submitting (Enter or the Send
 * atom) appends a message via the runtime. Pair with ComposerInput
 * + ComposerSend / ComposerCancel for a custom layout, or use the
 * compound `Composer` below for the default Pivox bar.
 */
type ComposerFormProps = ComponentProps<typeof ComposerPrimitive.Root>;
const ComposerForm: FC<ComposerFormProps> = ({ className, ...props }) => (
  <ComposerPrimitive.Root
    className={cn(
      'flex w-full items-end gap-2 border-t border-border bg-background p-3',
      className,
    )}
    {...props}
  />
);

/**
 * ComposerInput is a Pivox-styled Textarea slotted into
 * ComposerPrimitive.Input via `asChild`. Consumers can replace it
 * entirely if they want their own input (e.g. wire @pivox/primitives
 * PromptInput for attachments/tools — out of scope for v1).
 */
type ComposerInputProps = ComponentProps<typeof Textarea>;
const ComposerInput: FC<ComposerInputProps> = ({ className, ...props }) => (
  // Note: no `autoFocus`. jsx-a11y/no-autofocus is enforced because
  // auto-focusing a textarea on mount can hijack the user's keyboard
  // context (e.g. when navigating via screen reader). Consumers that
  // want focus on mount should pass `ref` and call `.focus()` from
  // an effect, scoped to a deliberate interaction (route load, send
  // action complete, etc.).
  <ComposerPrimitive.Input asChild>
    <Textarea
      rows={1}
      placeholder="Send a message…"
      className={cn('min-h-[44px] resize-none', className)}
      {...props}
    />
  </ComposerPrimitive.Input>
);

const ComposerSend: FC<ComponentProps<typeof Button>> = ({
  className,
  children,
  ...props
}) => (
  <ComposerPrimitive.Send asChild>
    <Button
      type="submit"
      size="icon"
      aria-label="Send message"
      className={cn('shrink-0', className)}
      {...props}
    >
      {children ?? <SendHorizonalIcon className="size-4" />}
    </Button>
  </ComposerPrimitive.Send>
);

const ComposerCancel: FC<ComponentProps<typeof Button>> = ({
  className,
  children,
  ...props
}) => (
  <ComposerPrimitive.Cancel asChild>
    <Button
      type="button"
      size="icon"
      variant="secondary"
      aria-label="Stop generation"
      className={cn('shrink-0', className)}
      {...props}
    >
      {children ?? <SquareIcon className="size-4" />}
    </Button>
  </ComposerPrimitive.Cancel>
);

/* ─── Run-state conditionals ─────────────────────────────────── */

// v0.14: ThreadPrimitive.If is deprecated in favor of AuiIf with a
// state selector. We expose two named variants — IfRunning,
// IfNotRunning — instead of a single boolean prop on If, matching
// architecture-avoid-boolean-props.
const IfRunning: FC<{ children: ReactNode }> = ({ children }) => (
  <AuiIf condition={(s) => s.thread.isRunning}>{children}</AuiIf>
);

const IfNotRunning: FC<{ children: ReactNode }> = ({ children }) => (
  <AuiIf condition={(s) => !s.thread.isRunning}>{children}</AuiIf>
);

/* ─── Default composer (compound) ────────────────────────────── */

/**
 * Default Composer bar — input + Send/Cancel button that swaps based
 * on run state. Composes the atoms above; consumers who want a
 * different layout drop this and build from ComposerForm + atoms
 * directly.
 */
const Composer: FC = () => (
  <ComposerForm>
    <div className="mx-auto flex w-full max-w-3xl items-end gap-2">
      <ComposerInput />
      <IfNotRunning>
        <ComposerSend />
      </IfNotRunning>
      <IfRunning>
        <ComposerCancel />
      </IfRunning>
    </div>
  </ComposerForm>
);

/* ─── Namespace export ───────────────────────────────────────── */

export const Chat = {
  Provider: ChatProvider,
  Root,
  Viewport,
  Empty,
  Messages,
  UserMessage,
  AssistantMessage,
  Composer,
  ComposerForm,
  ComposerInput,
  ComposerSend,
  ComposerCancel,
  IfRunning,
  IfNotRunning,
  Context: ChatContext,
};
