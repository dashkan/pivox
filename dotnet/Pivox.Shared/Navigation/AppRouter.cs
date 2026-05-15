namespace Pivox.Shared.Navigation;

/// <summary>
/// App-wide route state. One instance per process, owned by the platform
/// composition root (<c>AppDelegate</c> on macOS, <c>App.OnLaunched</c> on
/// Windows). Each platform layer subscribes to <see cref="CurrentChanged"/>
/// and renders the appropriate window/page; the router itself has zero
/// platform deps so the state machine stays single-sourced.
///
/// Operations:
/// <list type="bullet">
/// <item><see cref="Push"/> — add to history (back-navigable).</item>
/// <item><see cref="Pop"/> — back navigation.</item>
/// <item><see cref="Replace"/> — swap current without changing history depth.</item>
/// <item><see cref="ReplaceRoot"/> — clear history and start fresh (used at
/// the auth boundary so signing out wipes shell history).</item>
/// </list>
///
/// <para><b>Threading.</b> Mutations and event delivery are marshaled to
/// the <see cref="SynchronizationContext"/> captured at construction
/// (the UI thread on both platforms). Callers can <see cref="Push"/> /
/// <see cref="Pop"/> from any thread; the router posts the work to the
/// UI thread, mutates state there, and fires <see cref="CurrentChanged"/>
/// from there. Subscribers therefore never need to marshal — they're
/// already on the UI thread when their handler runs.</para>
///
/// <para><b>Eventual consistency.</b> Because mutations are posted (not
/// synchronously executed) when called from a background thread,
/// <see cref="Current"/> read immediately after <see cref="Push"/> from a
/// background thread may still show the old value until the post lands.
/// Reads from the UI thread are always current. This is the standard
/// async-router pattern; the alternative (lock-and-mutate-sync, post-the-
/// event) wins nothing and adds the lock.</para>
/// </summary>
public sealed class AppRouter
{
    private readonly List<AppRoute> _history;
    private readonly SynchronizationContext _uiContext;

    /// <param name="initial">Starting route. Becomes the root of the
    /// history stack.</param>
    /// <param name="uiContext">Optional UI <see cref="SynchronizationContext"/>
    /// to marshal mutations and events through. If omitted, the current
    /// thread's <see cref="SynchronizationContext.Current"/> is captured —
    /// which means the router MUST be constructed on the platform's UI
    /// thread. Tests pass an explicit context (or a same-thread stub).</param>
    /// <exception cref="InvalidOperationException">No
    /// <see cref="SynchronizationContext"/> is available — constructed off
    /// the UI thread without an explicit context.</exception>
    public AppRouter(AppRoute initial, SynchronizationContext? uiContext = null)
    {
        ArgumentNullException.ThrowIfNull(initial);
        _history = new List<AppRoute> { initial };
        _uiContext = uiContext
                     ?? SynchronizationContext.Current
                     ?? throw new InvalidOperationException(
                         "AppRouter must be constructed on a thread with a UI " +
                         "SynchronizationContext (typically the platform's UI " +
                         "thread), or with an explicit context passed in.");
    }

    /// <summary>The route currently being shown.</summary>
    public AppRoute Current => _history[^1];

    /// <summary>Read-only view of the history stack, oldest first.
    /// <c>History[^1] == Current</c>.</summary>
    public IReadOnlyList<AppRoute> History => _history;

    /// <summary>True when <see cref="Pop"/> would change the route — i.e.
    /// there's a previous entry to go back to.</summary>
    public bool CanGoBack => _history.Count > 1;

    /// <summary>Fires whenever <see cref="Current"/> changes. Always
    /// delivered on the UI thread (the <see cref="SynchronizationContext"/>
    /// captured at construction). Subscribers can touch UI directly
    /// without dispatching.</summary>
    public event EventHandler<AppRoute>? CurrentChanged;

    /// <summary>Navigate forward to a new route, keeping history.</summary>
    public void Push(AppRoute route)
    {
        ArgumentNullException.ThrowIfNull(route);
        OnUI(() =>
        {
            _history.Add(route);
            Notify();
        });
    }

    /// <summary>Back-navigate one step. No-op when <see cref="CanGoBack"/>
    /// is false — callers should gate UI affordances on
    /// <c>CanGoBack</c> rather than guarding here.</summary>
    public void Pop()
    {
        OnUI(() =>
        {
            if (_history.Count <= 1) return;
            _history.RemoveAt(_history.Count - 1);
            Notify();
        });
    }

    /// <summary>Replace the current route without changing history depth.
    /// Useful when a route's args change but the conceptual location
    /// doesn't (e.g., switching active org within the Shell).</summary>
    public void Replace(AppRoute route)
    {
        ArgumentNullException.ThrowIfNull(route);
        OnUI(() =>
        {
            _history[^1] = route;
            Notify();
        });
    }

    /// <summary>Clear history and set a new root. The transition that
    /// invalidates everything below — sign-in (history → [Shell]) and
    /// sign-out (history → [Login]). After this you cannot back-navigate
    /// to anything that was on the stack.</summary>
    public void ReplaceRoot(AppRoute route)
    {
        ArgumentNullException.ThrowIfNull(route);
        OnUI(() =>
        {
            _history.Clear();
            _history.Add(route);
            Notify();
        });
    }

    private void OnUI(Action action)
    {
        // Fast path: already on the UI thread → execute synchronously so
        // single-threaded callers get familiar "mutation visible by the
        // time the call returns" semantics. Background callers fall back
        // to Post → eventual consistency, documented above.
        if (SynchronizationContext.Current == _uiContext)
        {
            action();
        }
        else
        {
            _uiContext.Post(_ => action(), null);
        }
    }

    private void Notify() => CurrentChanged?.Invoke(this, Current);
}
