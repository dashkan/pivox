using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Pivox.Ai;
using Pivox.Auth;
using Pivox.Client;
using Pivox.Persistence;
using Pivox.Shared.Ai;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;
using Pivox.Shared.Organization;
using Pivox.Shared.Persistence;

namespace Pivox;

/// <summary>
/// Composition root. Owns the auth service, gRPC client, router,
/// remembered-email storage, key-value store, active-organization
/// holder, and chat service for the process lifetime. Mirrors
/// macOS <c>AppDelegate</c>'s wiring:
///
/// <list type="bullet">
/// <item><c>auth.CurrentChanged</c> → <c>router.ReplaceRoot(Login|Shell)</c></item>
/// <item><c>router.CurrentChanged</c> → build + activate the window
///   for the current route, close the previous.</item>
/// </list>
///
/// Construction order matters: <see cref="ActiveOrganization"/>
/// captures <see cref="SynchronizationContext.Current"/> at
/// construction (Rule 12) and throws if absent. WinUI 3 installs a
/// dispatcher-backed sync context on the UI thread automatically —
/// <c>OnLaunched</c> is on the UI thread, so the capture succeeds.
/// Construct the store, then the holder, before any other service
/// can hand a background-thread reference into the holder.
/// </summary>
public partial class App : Application
{
    private WindowsAuthService? _auth;
    private PivoxClient? _pivox;
    private AppRouter? _router;
    private RememberedEmail? _rememberedEmail;
    private IKeyValueStore? _keyValueStore;
    private ActiveOrganization? _activeOrganization;
    private IChatService? _chat;
    private Window? _currentWindow;

    public App()
    {
        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        // Build the persistence stack first so RememberedEmail and
        // ActiveOrganization share the single store instance.
        // ActiveOrganization captures SynchronizationContext.Current
        // on construction (Rule 12) — this is the UI thread because
        // OnLaunched runs on it, so the capture succeeds. Order
        // relative to the other constructors below is composition
        // hygiene, not a Rule 12 requirement (every constructor here
        // runs on the same thread).
        _keyValueStore = new ApplicationDataKeyValueStore();
        _activeOrganization = new ActiveOrganization(_keyValueStore);

        _auth = new WindowsAuthService();
        _pivox = new PivoxClient(_auth);
        // WindowsChatService is stateless re: organization — it takes
        // organizationName per-call (Phase B step 2a signature change).
        // Constructed here so the composition root owns its lifetime;
        // TODO(Phase B step 2b): wire _chat + _activeOrganization
        // into the chat-panel viewmodel inside BuildShellWindow when
        // the panel lands.
        _chat = new WindowsChatService(_pivox);
        _rememberedEmail = new RememberedEmail(_keyValueStore);

        _router = new AppRouter(new AppRoute.Login());

        _auth.CurrentChanged += OnAuthChanged;
        _router.CurrentChanged += OnRouteChanged;

        // Defer the initial render by one dispatcher tick. The
        // WindowsAuthService constructor dispatches the Firebase
        // auth-state-restore event via TryEnqueue; if we rendered
        // immediately we'd show Login for one frame before the
        // handler swaps to Shell. Deferring lets the auth state
        // resolve first — if there's a persisted session, the
        // handler fires OnAuthChanged → ReplaceRoot(Shell) before
        // the deferred render runs, and _currentWindow is already
        // set. If there's no session, the handler is a no-op and
        // the deferred render shows Login with no flicker.
        //
        // TryEnqueue can return false if the dispatcher is already
        // shutting down (the OS rejected the activation, the app is
        // being killed during launch). In that path we render
        // synchronously — accepting the persisted-session flicker
        // because the alternative is a blank window forever.
        if (!Microsoft.UI.Dispatching.DispatcherQueue.GetForCurrentThread()
                .TryEnqueue(() =>
                {
                    if (_currentWindow is null)
                        RenderRoute(_router!.Current);
                }))
        {
            RenderRoute(_router.Current);
        }
    }

    private void OnAuthChanged(object? sender, AuthSession? session)
    {
        // Defensive: the handler can only fire after _router is wired
        // in OnLaunched, so _router is non-null in practice — but
        // checking up-front skips the (otherwise-wasted) AppRoute
        // allocation and locks the precondition in close to the
        // returning branch.
        if (_router is null) return;

        // Guard against same-route swap. CurrentChanged fires not
        // only on real auth transitions (sign-in / sign-out) but also
        // on every token rotation — the dedup at SetCurrent compares
        // IdTokens, and force-refresh always produces a new JWT even
        // when the user is unchanged. Calling ReplaceRoot(Shell) when
        // we're already on Shell would rebuild the window and tear
        // down the old one — a brief windowless flicker on every
        // rotation. Compare routes via record equality before
        // invoking the swap. Mirrors macOS AppDelegate.OnAuthChanged.
        var desired = session is null
            ? (AppRoute)new AppRoute.Login()
            : new AppRoute.Shell();
        if (_router.Current.Equals(desired)) return;
        _router.ReplaceRoot(desired);
    }

    private void OnRouteChanged(object? sender, AppRoute route)
        => RenderRoute(route);

    private void RenderRoute(AppRoute route)
    {
        var previous = _currentWindow;

        _currentWindow = route switch
        {
            AppRoute.Login => BuildAuthWindow(
                new LoginPage(
                    new LoginViewModel(_auth!),
                    _router!,
                    _rememberedEmail!)),

            AppRoute.Register => BuildAuthWindow(
                new RegisterPage(
                    new RegisterViewModel(_auth!),
                    _router!)),

            AppRoute.Shell => BuildShellWindow(),

            _ => throw new InvalidOperationException(
                $"No window for route {route.GetType().Name}"),
        };

        _currentWindow.Activate();

        // Close the previous window AFTER activating the new one —
        // avoids a briefly windowless state. Don't Dispose — let GC
        // handle the managed peer (CLAUDE.md Rule 13).
        previous?.Close();
    }

    private Window BuildAuthWindow(Page page)
    {
        var window = new Window { Title = "Pivox" };
        window.SystemBackdrop = new Microsoft.UI.Xaml.Media.DesktopAcrylicBackdrop();
        window.ExtendsContentIntoTitleBar = true;

        var grid = new Grid();
        grid.RowDefinitions.Add(new RowDefinition { Height = new GridLength(32) });
        grid.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });

        // Drag region for the title bar.
        var titleBar = new Border { Margin = new Thickness(12, 0, 0, 0) };
        Grid.SetRow(titleBar, 0);
        grid.Children.Add(titleBar);
        window.SetTitleBar(titleBar);

        // Radial accent backdrop behind the card — spans the full
        // content area. Mirrors macOS RadialBackdropView.
        var backdrop = new UI.RadialBackdropElement();
        Grid.SetRow(backdrop, 1);
        grid.Children.Add(backdrop);

        // Auth page on top of the backdrop.
        Grid.SetRow(page, 1);
        grid.Children.Add(page);

        window.Content = grid;
        window.AppWindow.Resize(new Windows.Graphics.SizeInt32 { Width = 600, Height = 700 });
        return window;
    }

    private Window BuildShellWindow()
    {
        var window = new MainWindow(_auth!, _pivox!, _activeOrganization!);
        return window;
    }
}
