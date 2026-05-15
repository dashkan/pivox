using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Pivox.Auth;
using Pivox.Client;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;

namespace Pivox;

/// <summary>
/// Composition root. Owns the auth service, gRPC client, router,
/// and remembered-email storage for the process lifetime. Mirrors
/// macOS <c>AppDelegate</c>'s wiring:
///
/// <list type="bullet">
/// <item><c>auth.CurrentChanged</c> → <c>router.ReplaceRoot(Login|Shell)</c></item>
/// <item><c>router.CurrentChanged</c> → build + activate the window
///   for the current route, close the previous.</item>
/// </list>
/// </summary>
public partial class App : Application
{
    private WindowsAuthService? _auth;
    private PivoxClient? _pivox;
    private AppRouter? _router;
    private RememberedEmail? _rememberedEmail;
    private Window? _currentWindow;

    public App()
    {
        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        _auth = new WindowsAuthService();
        _pivox = new PivoxClient(_auth);
        _rememberedEmail = new RememberedEmail();

        // Router starts at Login; auth state listener swaps to Shell
        // if a persisted session is restored.
        _router = new AppRouter(new AppRoute.Login());

        _auth.CurrentChanged += OnAuthChanged;
        _router.CurrentChanged += OnRouteChanged;

        // Render the initial route (Login).
        RenderRoute(_router.Current);
    }

    private void OnAuthChanged(object? sender, AuthSession? session)
    {
        if (_router is null) return;

        if (session is not null)
        {
            _router.ReplaceRoot(new AppRoute.Shell());
        }
        else
        {
            _router.ReplaceRoot(new AppRoute.Login());
        }
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
        var window = new MainWindow(_auth!, _pivox!);
        return window;
    }
}
