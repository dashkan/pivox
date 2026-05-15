using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Auth;
using Pivox.Client;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;

namespace Pivox;

/// <summary>
/// Composition root. Owns the long-lived services
/// (<see cref="IAuthService"/>, <see cref="PivoxClient"/>,
/// <see cref="AppRouter"/>), wires the auth↔routing broker, and swaps
/// window controllers as routes change.
///
/// Routing model (phase 1):
/// <list type="bullet">
/// <item>Launch → if <c>auth.Current</c> is non-null,
///   <see cref="AppRoute.Shell"/>; otherwise <see cref="AppRoute.Login"/>.</item>
/// <item><c>auth.CurrentChanged</c> → <c>ReplaceRoot(Login|Shell)</c>.
///   Sign-out wipes any in-shell history (you can't back-navigate into
///   a signed-in shell after sign-out).</item>
/// </list>
///
/// <see cref="AppRouter"/> marshals events through the UI
/// <see cref="SynchronizationContext"/> captured at construction, so
/// the <c>CurrentChanged</c> handler here is always on the main thread.
/// </summary>
[Register("AppDelegate")]
public sealed class AppDelegate : NSApplicationDelegate
{
    // Strong refs so services, router, and window controllers don't
    // get GC'd while the app is running.
    private IAuthService? _auth;
    private PivoxClient? _pivox;
    private AppRouter? _router;
    private RememberedEmail? _rememberedEmail;
    private NSWindowController? _activeWindowController;

    public override void DidFinishLaunching(NSNotification notification)
    {
        NSApplication.SharedApplication.MainMenu = BuildMainMenu();

        // Long-lived services. Auth wraps FIRAuth + Google OAuth;
        // PivoxClient holds the gRPC channel with Bearer auto-attached;
        // RememberedEmail persists the email across launches via
        // NSUserDefaults.
        _auth = new MacOsAuthService();
        _pivox = new PivoxClient(_auth);
        _rememberedEmail = new RememberedEmail();

        // Router seeded with the route corresponding to current auth
        // state — covers the "Firebase restored a persisted session
        // before we got here" case without a flicker through Login.
        var initial = _auth.Current is null
            ? (AppRoute)new AppRoute.Login()
            : new AppRoute.Shell();
        _router = new AppRouter(initial);
        _router.CurrentChanged += OnRouteChanged;

        // Bridge auth state into routing. Login completion fires this,
        // sign-out fires this; the router decides which window to show.
        _auth.CurrentChanged += OnAuthChanged;

        // Render the initial route.
        RenderRoute(_router.Current);

        NSApplication.SharedApplication.ActivationPolicy = NSApplicationActivationPolicy.Regular;
        NSApplication.SharedApplication.Activate();
    }

    public override bool ApplicationShouldTerminateAfterLastWindowClosed(NSApplication sender) => true;

    // ───── auth ↔ router broker ──────────────────────────────────

    private void OnAuthChanged(object? sender, AuthSession? session)
    {
        // ReplaceRoot wipes history at the auth boundary. Sign-in
        // (Login → Shell) and sign-out (Shell → Login) both drop the
        // previous root entirely; you can't back-navigate across.
        //
        // Defensive: the handler can only fire after _router is wired
        // in DidFinishLaunching, so _router is non-null in practice —
        // but checking up-front skips the (otherwise-wasted) AppRoute
        // allocation and locks the precondition in close to the
        // returning branch.
        if (_router is null) return;

        // Guard against same-route swap: CurrentChanged fires not only
        // on real auth transitions but also on every token rotation
        // (the dedup at SetCurrent compares IdTokens, and force-refresh
        // always produces a new JWT even when the user is unchanged).
        // Calling ReplaceRoot(Shell) when we're already on Shell would
        // rebuild the window and tear down the old one — a second
        // launch-time flicker on every rotation. Compare routes via
        // record equality before invoking the swap.
        var desired = session is null
            ? (AppRoute)new AppRoute.Login()
            : new AppRoute.Shell();
        if (_router.Current.Equals(desired)) return;
        _router.ReplaceRoot(desired);
    }

    private void OnRouteChanged(object? sender, AppRoute route) => RenderRoute(route);

    // ───── route → window controller ─────────────────────────────

    private void RenderRoute(AppRoute route)
    {
        // Build the destination window controller first, then tear down
        // the old one. Reversing the order would briefly leave the app
        // windowless, which the OS reads as "all windows closed" — and
        // ApplicationShouldTerminateAfterLastWindowClosed returns true.
        var next = BuildWindowController(route);
        var previous = _activeWindowController;
        _activeWindowController = next;

        next.ShowWindow(null);
        next.Window?.MakeKeyAndOrderFront(null);

        // Close() runs the AppKit teardown (windowWillClose:, ref
        // release). Don't Dispose() the managed NSWindowController
        // explicitly — AppKit may still hold the native side briefly
        // (close animations, deferred release), and disposing the
        // managed peer while AppKit messages it would be a UAF.
        // The peer gets GC'd after AppKit's side is fully released.
        previous?.Close();
    }

    private NSWindowController BuildWindowController(AppRoute route)
    {
        return route switch
        {
            AppRoute.Login => new LoginWindowController(
                new LoginViewController(
                    new LoginViewModel(_auth!), _router!, _rememberedEmail!)),
            AppRoute.Register => new RegisterWindowController(
                new RegisterViewController(
                    new RegisterViewModel(_auth!), _router!)),
            AppRoute.Shell => BuildShellWindowController(),
            _ => throw new InvalidOperationException($"No window for route: {route}"),
        };
    }

    private NSWindowController BuildShellWindowController()
    {
        var sidebar = new SidebarViewController();
        var detail = new DetailViewController(_auth!, _pivox!);

        var split = new NSSplitViewController();
        split.AddSplitViewItem(NSSplitViewItem.CreateSidebar(sidebar));
        split.AddSplitViewItem(new NSSplitViewItem { ViewController = detail });

        var style = NSWindowStyle.Titled
                  | NSWindowStyle.Closable
                  | NSWindowStyle.Miniaturizable
                  | NSWindowStyle.Resizable;
        var window = new NSWindow(
            new CGRect(0, 0, 900, 600), style, NSBackingStore.Buffered, false)
        {
            Title = "Pivox",
            ContentViewController = split,
        };
        window.Center();
        return new NSWindowController(window);
    }

    // ───── menu ──────────────────────────────────────────────────

    private static NSMenu BuildMainMenu()
    {
        var main = new NSMenu();
        main.AddItem(BuildAppMenuItem());
        main.AddItem(BuildEditMenuItem());
        main.AddItem(BuildWindowMenuItem());
        return main;
    }

    private static NSMenuItem BuildAppMenuItem()
    {
        var appMenu = new NSMenu();
        appMenu.AddItem(new NSMenuItem("About Pivox", new Selector("orderFrontStandardAboutPanel:"), ""));
        appMenu.AddItem(NSMenuItem.SeparatorItem);
        appMenu.AddItem(new NSMenuItem("Hide Pivox", new Selector("hide:"), "h"));
        var hideOthers = new NSMenuItem("Hide Others", new Selector("hideOtherApplications:"), "h")
        {
            KeyEquivalentModifierMask = NSEventModifierMask.CommandKeyMask | NSEventModifierMask.AlternateKeyMask,
        };
        appMenu.AddItem(hideOthers);
        appMenu.AddItem(new NSMenuItem("Show All", new Selector("unhideAllApplications:"), ""));
        appMenu.AddItem(NSMenuItem.SeparatorItem);
        appMenu.AddItem(new NSMenuItem("Quit Pivox", new Selector("terminate:"), "q"));

        return new NSMenuItem { Submenu = appMenu };
    }

    private static NSMenuItem BuildEditMenuItem()
    {
        var editMenu = new NSMenu("Edit");
        editMenu.AddItem(new NSMenuItem("Undo", new Selector("undo:"), "z"));
        var redo = new NSMenuItem("Redo", new Selector("redo:"), "z")
        {
            KeyEquivalentModifierMask = NSEventModifierMask.CommandKeyMask | NSEventModifierMask.ShiftKeyMask,
        };
        editMenu.AddItem(redo);
        editMenu.AddItem(NSMenuItem.SeparatorItem);
        editMenu.AddItem(new NSMenuItem("Cut", new Selector("cut:"), "x"));
        editMenu.AddItem(new NSMenuItem("Copy", new Selector("copy:"), "c"));
        editMenu.AddItem(new NSMenuItem("Paste", new Selector("paste:"), "v"));
        editMenu.AddItem(new NSMenuItem("Select All", new Selector("selectAll:"), "a"));

        return new NSMenuItem("Edit") { Submenu = editMenu };
    }

    private static NSMenuItem BuildWindowMenuItem()
    {
        var windowMenu = new NSMenu("Window");
        windowMenu.AddItem(new NSMenuItem("Minimize", new Selector("performMiniaturize:"), "m"));
        windowMenu.AddItem(new NSMenuItem("Zoom", new Selector("performZoom:"), ""));
        windowMenu.AddItem(NSMenuItem.SeparatorItem);
        windowMenu.AddItem(new NSMenuItem("Bring All to Front", new Selector("arrangeInFront:"), ""));

        NSApplication.SharedApplication.WindowsMenu = windowMenu;
        return new NSMenuItem("Window") { Submenu = windowMenu };
    }
}
