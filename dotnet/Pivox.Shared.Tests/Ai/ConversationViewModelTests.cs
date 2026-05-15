using System.Collections.Specialized;
using Pivox.Shared.Ai;
using Pivox.Shared.Organization;
using Pivox.Shared.Tests.Persistence;
using Pivox.Shared.Tests.Threading;
using Xunit;

namespace Pivox.Shared.Tests.Ai;

/// <summary>
/// Tests for <see cref="ConversationViewModel"/>'s state machine and
/// streaming-message lifecycle. Drives a <see cref="StubChatService"/>
/// to control the event sequence; asserts state transitions, message
/// list mutations, and INotifyPropertyChanged signals.
///
/// Each test builds a fresh <see cref="ConversationViewModel"/> via
/// <see cref="BuildVm"/>, which also creates a backing
/// <see cref="ActiveOrganization"/> seeded with a default test org.
/// Tests that need to drive an org switch hold a reference to the
/// constructed <see cref="ActiveOrganization"/> via the second
/// return value.
///
/// <para><b>Threading model.</b> Every test body runs inside
/// <see cref="UiThread.Run"/>, which installs a single-threaded
/// <c>SynchronizationContext</c> on the test thread for the duration
/// of the async lambda. That matches production (AppKit main thread
/// / WinUI <c>DispatcherQueue</c>) and is what Rule 12-respecting
/// shared services (<see cref="ActiveOrganization"/>,
/// <see cref="ConversationViewModel"/>) expect at construction. Without
/// it, xUnit's <c>AsyncTestSyncContext</c> dispatches Posts to the
/// ThreadPool, which produces test-only ordering races that don't
/// exist in production.</para>
/// </summary>
public class ConversationViewModelTests
{
    private const string DefaultOrg = "organizations/test";

    /// <summary>Build a viewmodel + its backing active-organization
    /// reference. The default test org is pre-seeded so Send paths
    /// don't fail the "no organization selected" guard. Must be
    /// called from within <see cref="UiThread.Run"/> so the
    /// captured <c>SynchronizationContext</c> identity matches the
    /// test body's.</summary>
    private static (ConversationViewModel Vm, ActiveOrganization Org) BuildVm(
        IChatService chat, string? initialOrg = DefaultOrg)
    {
        var store = new InMemoryKeyValueStore();
        var org = new ActiveOrganization(store) { Current = initialOrg };
        return (new ConversationViewModel(chat, org), org);
    }

    [Fact]
    public void Initial_State_IsIdle_WithEmptyMessages() => UiThread.Run(() =>
    {
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);

        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Empty(vm.Messages);
        Assert.Null(vm.LastErrorKind);
        Assert.True(vm.CanSend);
        return Task.CompletedTask;
    });

    [Fact]
    public void SendAsync_TransitionsLoading_ThenStreaming_ThenIdle() => UiThread.Run(async () =>
    {
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);

        var states = new List<ConversationState>();
        vm.PropertyChanged += (_, args) =>
        {
            if (args.PropertyName == nameof(vm.State)) states.Add(vm.State);
        };

        // Start the send; the VM will sit in Loading until we emit
        // the first event.
        var sendTask = vm.SendAsync("hello");

        // Synchronously inspect state right after Send completes its
        // initial transitions. The VM appends the user message and
        // moves to Loading before awaiting the channel.
        Assert.Equal(ConversationState.Loading, vm.State);
        Assert.Single(vm.Messages);
        Assert.Equal(MessageRole.User, vm.Messages[0].Role);
        Assert.Equal("hello", vm.Messages[0].Text);

        // Stream a complete text track: Start → Delta → Delta → End.
        stub.Emit(new TextStartEvent("msg-123"));
        stub.Emit(new TextDeltaEvent("Hi "));
        stub.Emit(new TextDeltaEvent("there!"));
        stub.Emit(new TextEndEvent());
        stub.Complete();

        await sendTask;

        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Equal(2, vm.Messages.Count);
        Assert.Equal(MessageRole.Assistant, vm.Messages[1].Role);
        Assert.Equal("Hi there!", vm.Messages[1].Text);
        Assert.Equal("msg-123", vm.Messages[1].MessageId);

        // State transitions in order: Loading → Streaming → Idle.
        Assert.Equal(
            new[]
            {
                ConversationState.Loading,
                ConversationState.Streaming,
                ConversationState.Idle,
            },
            states);
    });

    [Fact]
    public void TextDelta_MutatesInflightMessage_FiresPropertyChanged() => UiThread.Run(async () =>
    {
        // Verifies the placeholder-message streaming pattern: the
        // assistant message is appended once on TextStart, then its
        // Text mutates in place on each Delta. UI subscribers see
        // per-row PropertyChanged on Message, not list-level churn.
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);
        var sendTask = vm.SendAsync("ping");

        stub.Emit(new TextStartEvent("m1"));
        // After TextStart we have a placeholder; wait for the
        // consumer to process it before subscribing.
        await WaitForStateAsync(vm, ConversationState.Streaming);
        var inflight = vm.Messages.Last(m => m.Role == MessageRole.Assistant);

        var textChanges = 0;
        inflight.PropertyChanged += (_, args) =>
        {
            if (args.PropertyName == nameof(Message.Text)) textChanges++;
        };

        stub.Emit(new TextDeltaEvent("a"));
        stub.Emit(new TextDeltaEvent("b"));
        stub.Emit(new TextDeltaEvent("c"));
        stub.Emit(new TextEndEvent());
        stub.Complete();
        await sendTask;

        Assert.Equal(3, textChanges);
        Assert.Equal("abc", inflight.Text);
    });

    [Fact]
    public void ServiceThrowsChatException_TransitionsToError() => UiThread.Run(async () =>
    {
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);
        var sendTask = vm.SendAsync("hi");

        stub.Throw(new ChatException(
            ChatErrorKind.Network,
            "stream interrupted"));

        await sendTask;

        Assert.Equal(ConversationState.Error, vm.State);
        Assert.Equal(ChatErrorKind.Network, vm.LastErrorKind);
        Assert.Equal("stream interrupted", vm.LastErrorMessage);
        // CanSend stays true so the user can retry.
        Assert.True(vm.CanSend);
    });

    [Fact]
    public void ServiceThrowsGenericException_WrapsAsServerError() => UiThread.Run(async () =>
    {
        // Defense-in-depth: implementations should wrap as
        // ChatException, but if one slips through, the VM categorizes
        // it as Server rather than surfacing raw exception types.
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);
        var sendTask = vm.SendAsync("hi");

        stub.Throw(new InvalidOperationException("oops"));

        await sendTask;

        Assert.Equal(ConversationState.Error, vm.State);
        Assert.Equal(ChatErrorKind.Server, vm.LastErrorKind);
    });

    [Fact]
    public void Cancel_DuringStreaming_TransitionsToIdle_KeepsPartialContent() => UiThread.Run(async () =>
    {
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);
        var sendTask = vm.SendAsync("hi");

        stub.Emit(new TextStartEvent("m1"));
        stub.Emit(new TextDeltaEvent("partial"));

        // Drain queued event posts before the assertion. On the
        // single-threaded UiThread context, the channel reader's
        // continuations are queued behind us; awaiting state pumps
        // them through.
        //
        // Audit L5: pin the intermediate Streaming state explicitly
        // — the assertion below ("transitions to Idle") is only
        // meaningful if we were observably Streaming first. Without
        // this, a regression that skipped Streaming entirely (e.g.,
        // a refactor that goes Loading → Idle directly when cancel
        // fires) would still satisfy the post-cancel checks.
        await WaitForStateAsync(vm, ConversationState.Streaming);
        Assert.Equal(ConversationState.Streaming, vm.State);
        Assert.Equal(2, vm.Messages.Count);
        Assert.Equal("partial", vm.Messages[1].Text);

        vm.Cancel();
        await sendTask;

        Assert.Equal(ConversationState.Idle, vm.State);
        // The assistant message with partial content stays in the
        // transcript — user sees what arrived before they cancelled.
        Assert.Equal(2, vm.Messages.Count);
        Assert.Equal("partial", vm.Messages[1].Text);
    });

    [Fact]
    public void Cancel_BeforeFirstDelta_DiscardsEmptyPlaceholder() => UiThread.Run(async () =>
    {
        // Edge case: cancel after TextStart but before any deltas.
        // The placeholder is empty; rather than show a blank
        // assistant row, the VM drops it.
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);
        var sendTask = vm.SendAsync("hi");

        stub.Emit(new TextStartEvent("m1"));
        await WaitForStateAsync(vm, ConversationState.Streaming);
        vm.Cancel();
        await sendTask;

        Assert.Equal(ConversationState.Idle, vm.State);
        // Only the user message remains.
        Assert.Single(vm.Messages);
        Assert.Equal(MessageRole.User, vm.Messages[0].Role);
    });

    [Fact]
    public void ReSend_AfterError_AllowedAndClearsError() => UiThread.Run(async () =>
    {
        // StubChatService is single-shot — Throw closes the channel
        // with an exception, so the same instance can't serve a retry.
        // Use a RetryableChatService that swaps to a fresh inner stub
        // when reset, mirroring how a real service handles successive
        // calls on the same channel.
        var failing = new StubChatService();
        var retry = new RoutingChatService(failing);
        var (vm, _) = BuildVm(retry);

        // First call: fail.
        var first = vm.SendAsync("hi");
        failing.Throw(new ChatException(ChatErrorKind.Network, "boom"));
        await first;

        Assert.Equal(ConversationState.Error, vm.State);
        Assert.NotNull(vm.LastErrorKind);

        // Swap in a fresh stub and retry.
        var succeeding = new StubChatService();
        retry.SetInner(succeeding);
        var second = vm.SendAsync("hi again");
        Assert.Equal(ConversationState.Loading, vm.State);
        Assert.Null(vm.LastErrorKind);
        succeeding.Emit(new TextStartEvent("m2"));
        succeeding.Emit(new TextDeltaEvent("ok"));
        succeeding.Emit(new TextEndEvent());
        succeeding.Complete();
        await second;

        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Equal(1, failing.InvocationCount);
        Assert.Equal(1, succeeding.InvocationCount);
    });

    [Fact]
    public void SendAsync_WhileStreaming_Throws() => UiThread.Run(async () =>
    {
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);
        var first = vm.SendAsync("hi");

        stub.Emit(new TextStartEvent("m1"));
        await WaitForStateAsync(vm, ConversationState.Streaming);

        await Assert.ThrowsAsync<InvalidOperationException>(
            () => vm.SendAsync("nope"));

        // Clean up so the stub doesn't leak.
        stub.Emit(new TextEndEvent());
        stub.Complete();
        await first;
    });

    /// <summary>Spins (with a short timeout cap) until the viewmodel's
    /// <see cref="ConversationViewModel.State"/> reaches the expected
    /// value. Required because the iterator's channel reader processes
    /// emitted events asynchronously via continuations posted to the
    /// UiThread sync context — those continuations don't run until the
    /// test method yields (await). This helper yields in a loop until
    /// the expected state is observed.
    ///
    /// The 200ms cap protects against a truly stuck consumer; in
    /// practice state flips in microseconds once the sync context
    /// pumps.</summary>
    private static async Task WaitForStateAsync(
        ConversationViewModel vm,
        ConversationState expected,
        int timeoutMs = 200)
    {
        var deadline = Environment.TickCount + timeoutMs;
        while (vm.State != expected && Environment.TickCount < deadline)
        {
            await Task.Delay(5);
        }
        Assert.Equal(expected, vm.State);
    }

    [Fact]
    public void SendAsync_TurnsSentToService_IncludeFullTranscript() => UiThread.Run(async () =>
    {
        // Stateless calls send the full transcript each time so the
        // server has conversation context without a stored
        // conversation id. Verify the VM projects Messages → ChatTurn
        // correctly: first call sends just the user turn; second call
        // sends user-1, assistant-1, user-2 in order.
        //
        // Uses RoutingChatService to swap stubs between calls on a
        // single VM.
        var firstStub = new StubChatService();
        var routing = new RoutingChatService(firstStub);
        var (vm, _) = BuildVm(routing);

        var t1 = vm.SendAsync("first user");
        firstStub.Emit(new TextStartEvent("m1"));
        firstStub.Emit(new TextDeltaEvent("first reply"));
        firstStub.Emit(new TextEndEvent());
        firstStub.Complete();
        await t1;

        Assert.NotNull(firstStub.LastTurnsSent);
        Assert.Single(firstStub.LastTurnsSent);
        Assert.Equal(MessageRole.User, firstStub.LastTurnsSent![0].Role);
        Assert.Equal("first user", firstStub.LastTurnsSent[0].Text);

        // Swap in a fresh stub for the second call.
        var secondStub = new StubChatService();
        routing.SetInner(secondStub);

        var t2 = vm.SendAsync("second user");
        secondStub.Emit(new TextStartEvent("m2"));
        secondStub.Emit(new TextDeltaEvent("second reply"));
        secondStub.Emit(new TextEndEvent());
        secondStub.Complete();
        await t2;

        // Second call's turns include the full transcript so far:
        // first user → first reply → second user.
        Assert.NotNull(secondStub.LastTurnsSent);
        Assert.Equal(3, secondStub.LastTurnsSent!.Count);
        Assert.Equal(
            (MessageRole.User, "first user"),
            (secondStub.LastTurnsSent[0].Role, secondStub.LastTurnsSent[0].Text));
        Assert.Equal(
            (MessageRole.Assistant, "first reply"),
            (secondStub.LastTurnsSent[1].Role, secondStub.LastTurnsSent[1].Text));
        Assert.Equal(
            (MessageRole.User, "second user"),
            (secondStub.LastTurnsSent[2].Role, secondStub.LastTurnsSent[2].Text));
    });

    [Fact]
    public void CanSend_PropertyChanged_FiresOnTransitions() => UiThread.Run(async () =>
    {
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);

        var canSendChanges = 0;
        vm.PropertyChanged += (_, args) =>
        {
            if (args.PropertyName == nameof(vm.CanSend)) canSendChanges++;
        };

        var sendTask = vm.SendAsync("hi");
        // Loading: CanSend false (1 change)
        // Streaming on TextStart: CanSend still false but property
        // fires defensively (we raise on every state change). 1-3.
        stub.Emit(new TextStartEvent("m1"));
        stub.Emit(new TextEndEvent());
        // Idle: CanSend true (final).
        stub.Complete();
        await sendTask;

        // We don't pin an exact count — implementation may collapse
        // some — but we require at least 2 (off and back on) and that
        // CanSend ends true.
        Assert.True(canSendChanges >= 2, $"CanSend changed {canSendChanges} times");
        Assert.True(vm.CanSend);
    });

    [Fact]
    public void Messages_CollectionChanged_FiresOnAddOnly() => UiThread.Run(async () =>
    {
        // The ObservableCollection should fire CollectionChanged only
        // when a Message is appended (user message at Send, assistant
        // placeholder at TextStart) — NOT for in-place text mutations
        // during streaming.
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub);

        var addCount = 0;
        ((INotifyCollectionChanged)vm.Messages).CollectionChanged += (_, args) =>
        {
            if (args.Action == NotifyCollectionChangedAction.Add) addCount++;
        };

        var sendTask = vm.SendAsync("hi");
        stub.Emit(new TextStartEvent("m1"));
        stub.Emit(new TextDeltaEvent("a"));
        stub.Emit(new TextDeltaEvent("b"));
        stub.Emit(new TextDeltaEvent("c"));
        stub.Emit(new TextEndEvent());
        stub.Complete();
        await sendTask;

        // 1 add for the user message, 1 for the assistant placeholder.
        // No further adds despite three deltas — those mutate Text on
        // the existing assistant Message.
        Assert.Equal(2, addCount);
    });

    [Fact]
    public void SendAsync_PassesActiveOrganizationToService() => UiThread.Run(async () =>
    {
        // Pins the per-call organization-name contract: the VM reads
        // ActiveOrganization.Current at SendAsync entry and forwards
        // it to the service. WinUI's implementation honors the same
        // contract.
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub, initialOrg: "organizations/acme");

        var sendTask = vm.SendAsync("hi");
        stub.Emit(new TextStartEvent("m1"));
        stub.Emit(new TextDeltaEvent("hello"));
        stub.Emit(new TextEndEvent());
        stub.Complete();
        await sendTask;

        Assert.Equal("organizations/acme", stub.LastOrganizationSent);
    });

    [Fact]
    public void SendAsync_WithNoActiveOrganization_FailsWithError() => UiThread.Run(async () =>
    {
        // Defensive: if SendAsync is invoked while ActiveOrganization
        // is null (e.g., a UI race where the composer didn't gate),
        // the VM fails to Error rather than hitting the server with
        // a malformed request.
        //
        // Audit L6: pin the exact LastErrorMessage rather than a
        // substring match. The string is user-visible (the chat panel
        // surfaces it in the error banner) and a copy edit should
        // require an intentional test update, not a silent drift.
        // Pin the ChatErrorKind to NoOrganization too so a refactor
        // that picks the wrong kind (e.g., AuthenticationRequired)
        // fails the test even if the message text happens to align.
        var stub = new StubChatService();
        var (vm, _) = BuildVm(stub, initialOrg: null);

        await vm.SendAsync("hi");

        Assert.Equal(ConversationState.Error, vm.State);
        Assert.Equal(ChatErrorKind.NoOrganization, vm.LastErrorKind);
        Assert.Equal(
            "Select an organization before sending a message.",
            vm.LastErrorMessage);
        Assert.Equal(0, stub.InvocationCount);
    });

    [Fact]
    public void OrganizationChange_WhileStreaming_CancelsAndClearsMessages() => UiThread.Run(async () =>
    {
        // Org switch mid-stream: cancellation propagates through the
        // service's async iterator, the cancel-handler in SendAsync
        // resets state to Idle, and OnActiveOrganizationChanged
        // clears Messages. Net visible state: empty transcript, Idle.
        //
        // Because the test body runs on UiThread's single-threaded
        // sync context — the same context ActiveOrganization
        // captured at construction — the setter takes the fast
        // synchronous path. OnActiveOrganizationChanged runs inline
        // BEFORE the setter returns, exactly as it does on a
        // production UI thread.
        var stub = new StubChatService();
        var (vm, org) = BuildVm(stub);

        var sendTask = vm.SendAsync("hello");
        stub.Emit(new TextStartEvent("m1"));
        stub.Emit(new TextDeltaEvent("partial"));
        await WaitForStateAsync(vm, ConversationState.Streaming);

        // Synchronous fast-path: Cancel + Messages.Clear all happen
        // before this assignment returns.
        org.Current = "organizations/other";

        Assert.Empty(vm.Messages);
        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Null(vm.LastErrorKind);

        // Then the iterator's catch block runs on its next continuation
        // — drain it so the test doesn't leak the in-flight Task.
        await sendTask;
        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Empty(vm.Messages);
    });

    [Fact]
    public void OrganizationChange_WhileError_ClearsErrorAndMessages() => UiThread.Run(async () =>
    {
        // From the Error state: org switch resets to Idle and wipes
        // any leftover messages plus the error metadata. Fast-path
        // synchronous mutation under UiThread — assert immediately
        // after the setter returns.
        var stub = new StubChatService();
        var (vm, org) = BuildVm(stub);

        var sendTask = vm.SendAsync("hi");
        stub.Throw(new ChatException(ChatErrorKind.Network, "boom"));
        await sendTask;
        Assert.Equal(ConversationState.Error, vm.State);
        Assert.NotEmpty(vm.Messages);   // the user message remains

        org.Current = "organizations/other";

        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Empty(vm.Messages);
        Assert.Null(vm.LastErrorKind);
        Assert.Null(vm.LastErrorMessage);
        Assert.True(vm.CanSend);
    });

    [Fact]
    public void OrganizationChange_WhileIdle_IsNoOpOnState() => UiThread.Run(() =>
    {
        // No-stream, no-error: org switch leaves State at Idle.
        // Messages is already empty so Clear() is a no-op visible
        // outcome. We additionally pin that CanSend stays asserted
        // throughout — there's no spurious "send disabled" flicker
        // when switching orgs from an idle state. (Audit finding L4.)
        var stub = new StubChatService();
        var (vm, org) = BuildVm(stub);

        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Empty(vm.Messages);
        Assert.True(vm.CanSend);

        var canSendFalseObserved = false;
        vm.PropertyChanged += (_, args) =>
        {
            if (args.PropertyName == nameof(vm.CanSend) && !vm.CanSend)
            {
                canSendFalseObserved = true;
            }
        };

        org.Current = "organizations/other";

        Assert.Equal(ConversationState.Idle, vm.State);
        Assert.Empty(vm.Messages);
        Assert.Null(vm.LastErrorKind);
        Assert.True(vm.CanSend);
        Assert.False(
            canSendFalseObserved,
            "CanSend should not transition to false during an idle org switch.");
        return Task.CompletedTask;
    });
}
