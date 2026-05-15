using Microsoft.VisualStudio.Threading;

namespace Pivox.Shared.Tests.Threading;

/// <summary>
/// Test helper that runs an async body on the current thread with a
/// single-threaded <see cref="SynchronizationContext"/> installed —
/// mirroring the production runtime environment of an AppKit main
/// thread (macOS) or WinUI <c>DispatcherQueue</c> (Windows).
///
/// <para><b>Why this exists.</b> xUnit 2's
/// <c>AsyncTestSyncContext</c> delegates <see cref="SynchronizationContext.Post(SendOrPostCallback, object?)"/>
/// to the ThreadPool. Code that captures a <c>SynchronizationContext</c>
/// at construction (per Rule 12 — shared services that fire events
/// to the UI MUST capture the UI sync context and Post events
/// through it) then observes its callbacks running on whichever TP
/// worker happens to dequeue them, with
/// <c>SynchronizationContext.Current</c> set to <c>null</c>. That
/// breaks <c>SynchronizationContext.Current == _capturedContext</c>
/// fast-path checks, exposes test-only ordering races between
/// concurrent posts, and tests "pass" against a threading model
/// production never sees. The fixture replaces that with a real
/// single-threaded pump so test behavior matches production
/// behavior.</para>
///
/// <para><b>Usage.</b> Wrap the test body — including arrange,
/// act, and assert — in <see cref="Run(Func{Task})"/>. Inside the
/// delegate, <c>SynchronizationContext.Current</c> is the
/// <see cref="JoinableTaskContext"/>'s main-thread sync context;
/// every <c>await</c> resumes on that same context;
/// reference-equality checks against captured contexts hold.</para>
///
/// <code>
/// [Fact]
/// public void Whatever() => UiThread.Run(async () =>
/// {
///     var (vm, org) = BuildVm(...);
///     await vm.SendAsync("hi");
///     org.Current = "organizations/other";  // fast path; sync mutation
///     Assert.Empty(vm.Messages);            // no WaitForState needed
/// });
/// </code>
///
/// <para><b>Implementation note.</b> Each call constructs a fresh
/// <see cref="JoinableTaskContext"/> affinitized to the calling
/// thread. Test methods don't need to share one — JTCs are cheap
/// and per-test isolation eliminates accidental cross-test
/// coupling on the sync-context identity.</para>
/// </summary>
internal static class UiThread
{
    /// <summary>Run an async delegate to completion on the current
    /// thread with a single-threaded sync context installed.
    /// Exceptions from the delegate propagate to the caller.</summary>
    public static void Run(Func<Task> asyncBody)
    {
        ArgumentNullException.ThrowIfNull(asyncBody);
        using var jtc = new JoinableTaskContext();
        jtc.Factory.Run(asyncBody);
    }
}
