using System.ComponentModel;
using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Ai;
using Pivox.Auth;
using Pivox.Client;
using Pivox.MacOs.Persistence;
using Pivox.Shared.Ai;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;
using Pivox.Shared.Organization;
using Pivox.Shared.Persistence;

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
    private IKeyValueStore? _keyValueStore;
    private ActiveOrganization? _activeOrganization;
    private ChatPanelState? _chatPanelState;
    private IChatService? _chatService;
    private NSWindowController? _activeWindowController;

    // Live shell wiring — refreshed by BuildShellWindowController.
    // Used by the toolbar/menu action handlers to flip the chat
    // panel and observe the active organization.
    private NSSplitViewItem? _chatPanelSplitItem;
    private NSToolbarItem? _chatToggleToolbarItem;

    // KVO context for ChatPanelSplitItem.collapsed. The non-null
    // sentinel lets ObserveValue dispatch to the right handler when
    // multiple KVO observers exist (Apple's documented pattern —
    // see "Registering as an Observer of a Property").
    private static readonly IntPtr ChatPanelCollapsedKvoContext = (IntPtr)0xC0117A;

    public override void DidFinishLaunching(NSNotification notification)
    {
        NSApplication.SharedApplication.MainMenu = BuildMainMenu();

        // Long-lived services. Auth wraps FIRAuth + Google OAuth;
        // PivoxClient holds the gRPC channel with Bearer auto-attached;
        // RememberedEmail persists the email across launches via the
        // shared key-value store abstraction (NSUserDefaults-backed
        // on macOS). ActiveOrganization holds the currently-active
        // org resource name and persists it across launches via the
        // same store.
        _keyValueStore = new NsUserDefaultsKeyValueStore();
        _activeOrganization = new ActiveOrganization(_keyValueStore);
        _chatPanelState = new ChatPanelState(_keyValueStore);
        _auth = new MacOsAuthService();
        _pivox = new PivoxClient(_auth);
        _chatService = new MacOsChatService(_pivox);
        _rememberedEmail = new RememberedEmail(_keyValueStore);

        // Chat panel toggle gating: the toolbar item + ⌘⇧A menu
        // binding both validate against ActiveOrganization.Current,
        // so observe changes to enable/disable in real time.
        _activeOrganization.PropertyChanged += OnActiveOrganizationChanged;

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
        var detail = new DetailViewController(_auth!, _pivox!, _activeOrganization!);

        // Build the chat panel up front. ConversationViewModel
        // captures SynchronizationContext.Current at construction
        // (Rule 12) — DidFinishLaunching runs on the main thread,
        // so this is correct.
        var conversation = new ConversationViewModel(_chatService!, _activeOrganization!);
        var chatPanel = new ChatPanelViewController(conversation);

        var split = new NSSplitViewController();
        split.AddSplitViewItem(NSSplitViewItem.CreateSidebar(sidebar));
        split.AddSplitViewItem(new NSSplitViewItem { ViewController = detail });

        // Trailing inspector for chat. CanCollapse=true so the
        // toolbar toggle can hide it. Initial Collapsed reflects the
        // persisted ChatPanelState.IsVisible.
        _chatPanelSplitItem = new NSSplitViewItem
        {
            ViewController = chatPanel,
            CanCollapse = true,
            Collapsed = !_chatPanelState!.IsVisible,
            // Sensible inspector widths. AppKit autosaves the user's
            // resize across launches via NSSplitViewItem's persisted
            // state once the split view has an autosaveName.
            MinimumThickness = 280,
            MaximumThickness = 520,
        };
        split.AddSplitViewItem(_chatPanelSplitItem);

        // Subscribe to ChatPanelState changes so external writes
        // (future surface that flips it) update the split item.
        _chatPanelState.PropertyChanged += OnChatPanelStateChanged;

        // Observe drag-collapse from the user so ChatPanelState
        // stays in sync. Without this, dragging the divider to
        // collapse the chat pane sets `_chatPanelSplitItem.Collapsed
        // = true` but doesn't write back to IsVisible — the next
        // toolbar-toggle click then needs two presses to re-open
        // (first press flips IsVisible:true→false, which produces
        // no visual change because Collapsed is already true;
        // second press flips back to true and expands).
        //
        // KVO on NSSplitViewItem.collapsed is the documented hook
        // for this — Apple updates the property on user-driven
        // collapse and AppKit fires the observer notification.
        _chatPanelSplitItem.AddObserver(
            this,
            new NSString("collapsed"),
            NSKeyValueObservingOptions.New,
            ChatPanelCollapsedKvoContext);

        var style = NSWindowStyle.Titled
                  | NSWindowStyle.Closable
                  | NSWindowStyle.Miniaturizable
                  | NSWindowStyle.Resizable;
        var window = new NSWindow(
            new CGRect(0, 0, 1100, 680), style, NSBackingStore.Buffered, false)
        {
            Title = "Pivox",
            ContentViewController = split,
            Toolbar = BuildShellToolbar(),
        };
        // ToolbarStyle.Unified keeps the toolbar visually integrated
        // with the title bar on macOS 11+ — matches the macOS 26
        // design system for split-view-rooted windows.
        window.ToolbarStyle = NSWindowToolbarStyle.Unified;
        window.Center();
        return new NSWindowController(window);
    }

    private NSToolbar BuildShellToolbar()
    {
        // Single-item toolbar for now (chat panel toggle). NSToolbar
        // takes a delegate that vends items by identifier; we
        // implement it inline via a small delegate subclass below.
        var toolbar = new NSToolbar("pivox.shell.toolbar")
        {
            // Icon-only is the binding name for `NSToolbarDisplayModeIconOnly`
            // — the binding generator strips the redundant "Only" suffix.
            DisplayMode = NSToolbarDisplayMode.Icon,
            // ShowsBaselineSeparator is obsoleted on macOS 15+ (the new
            // toolbar style handles the separator implicitly via
            // NSWindowToolbarStyle); don't set it on macOS 26 target.
            AllowsUserCustomization = false,
            AutosavesConfiguration = false,
        };
        toolbar.Delegate = new ShellToolbarDelegate(this);
        return toolbar;
    }

    // Inline NSToolbarDelegate subclass — small enough that hoisting
    // it to a separate file would add noise. Owns no state of its
    // own; defers back to the AppDelegate for action wiring.
    private sealed class ShellToolbarDelegate : NSToolbarDelegate
    {
        internal const string ChatToggleId = "pivox.toolbar.chat-toggle";

        private readonly AppDelegate _app;

        public ShellToolbarDelegate(AppDelegate app) => _app = app;

        public override string[] AllowedItemIdentifiers(NSToolbar toolbar)
            => new[] { ChatToggleId, NSToolbar.NSToolbarFlexibleSpaceItemIdentifier };

        public override string[] DefaultItemIdentifiers(NSToolbar toolbar)
            => new[] { NSToolbar.NSToolbarFlexibleSpaceItemIdentifier, ChatToggleId };

        public override NSToolbarItem? WillInsertItem(
            NSToolbar toolbar, string itemIdentifier, bool willBeInserted)
        {
            if (itemIdentifier != ChatToggleId) return null;

            var item = new NSToolbarItem(ChatToggleId)
            {
                Label = "Chat",
                PaletteLabel = "Chat",
                ToolTip = "Toggle AI chat panel (⇧⌘A)",
                // SF Symbol: `sparkles` — the Pivox AI-surface glyph
                // (matches the SwiftUI native toolbar item). Hierarchical
                // rendering picks up the system tint subtly; in Phase C
                // this will swap to a rainbow-gradient fill via
                // AIShimmerLayer-style tinting on hover. Falls back
                // gracefully on absence (GetSystemSymbol returns null
                // → toolbar shows no image; tooltip + label still
                // identify the action).
                Image = NSImage.GetSystemSymbol("sparkles", "Toggle AI chat panel"),
                Bordered = true,
                Action = new Selector("toggleChatPanel:"),
                Target = _app,
            };
            _app._chatToggleToolbarItem = item;
            _app.RefreshChatToggleEnabled();
            return item;
        }
    }

    // ───── chat panel toggle wiring ─────────────────────────────

    private void OnActiveOrganizationChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName != nameof(ActiveOrganization.Current)) return;
        RefreshChatToggleEnabled();
    }

    private void OnChatPanelStateChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName != nameof(ChatPanelState.IsVisible)) return;
        ApplyChatPanelVisibility(_chatPanelState!.IsVisible, animated: true);
    }

    public override void ObserveValue(
        NSString keyPath, NSObject ofObject, NSDictionary change, IntPtr context)
    {
        if (context == ChatPanelCollapsedKvoContext
            && _chatPanelSplitItem is not null
            && _chatPanelState is not null
            && keyPath?.ToString() == "collapsed")
        {
            // User-driven divider drag → flip ChatPanelState so the
            // toolbar toggle stays consistent with the visual state.
            // Skip the write if already in sync (avoids a feedback
            // loop: setter → PropertyChanged → ApplyChatPanelVisibility
            // → Animator.Collapsed → KVO → setter again).
            var collapsed = _chatPanelSplitItem.Collapsed;
            var desiredVisible = !collapsed;
            if (_chatPanelState.IsVisible != desiredVisible)
            {
                _chatPanelState.IsVisible = desiredVisible;
            }
            return;
        }
        // Defensive null-forgiving: ObserveValue's keyPath parameter
        // is declared non-null in the binding, but the analyzer trips
        // on the post-null-check flow above. By this point keyPath is
        // either our matched path (handled and returned) or whatever
        // the base class needs — pass through.
        base.ObserveValue(keyPath!, ofObject, change, context);
    }

    private void ApplyChatPanelVisibility(bool visible, bool animated)
    {
        if (_chatPanelSplitItem is null) return;
        var collapsed = !visible;
        if (_chatPanelSplitItem.Collapsed == collapsed) return;

        if (animated)
        {
            NSAnimationContext.RunAnimation(
                ctx =>
                {
                    ctx.Duration = 0.2;
                    ((NSSplitViewItem)_chatPanelSplitItem.Animator).Collapsed = collapsed;
                },
                null);
        }
        else
        {
            _chatPanelSplitItem.Collapsed = collapsed;
        }
    }

    private void RefreshChatToggleEnabled()
    {
        if (_chatToggleToolbarItem is null) return;
        var hasOrg = !string.IsNullOrEmpty(_activeOrganization?.Current);
        _chatToggleToolbarItem.Enabled = hasOrg;
        // If org goes away while panel is open, collapse it. Avoids
        // the user staring at a panel that can't do anything.
        if (!hasOrg && _chatPanelState is not null && _chatPanelState.IsVisible)
        {
            _chatPanelState.IsVisible = false;
        }
    }

    [Export("toggleChatPanel:")]
    public void ToggleChatPanel(NSObject sender)
    {
        // Defense: if the user somehow triggers via menu while no
        // org is selected (validateMenuItem misses, the validation
        // path on NSMenuItem with a target=AppDelegate ought to
        // route to validateMenuItem: here, but be safe).
        if (string.IsNullOrEmpty(_activeOrganization?.Current)) return;
        _chatPanelState?.Toggle();
    }

    [Export("validateMenuItem:")]
    public bool ValidateMenuItem(NSMenuItem item)
    {
        if (item.Action?.Name == "toggleChatPanel:")
        {
            return !string.IsNullOrEmpty(_activeOrganization?.Current);
        }
        // Unknown selectors — defer to default behavior. AppKit's
        // responder chain will fall through to other validators.
        return true;
    }

    [Export("validateToolbarItem:")]
    public bool ValidateToolbarItem(NSToolbarItem item)
    {
        if (item.Action?.Name == "toggleChatPanel:")
        {
            return !string.IsNullOrEmpty(_activeOrganization?.Current);
        }
        return true;
    }

    // ───── menu ──────────────────────────────────────────────────

    private NSMenu BuildMainMenu()
    {
        var main = new NSMenu();
        main.AddItem(BuildAppMenuItem());
        main.AddItem(BuildEditMenuItem());
        main.AddItem(BuildViewMenuItem());
        main.AddItem(BuildWindowMenuItem());
        return main;
    }

    private NSMenuItem BuildViewMenuItem()
    {
        var viewMenu = new NSMenu("View");
        var toggleChat = new NSMenuItem(
            "Show Chat Panel",
            new Selector("toggleChatPanel:"),
            "a")
        {
            // ⇧⌘A: distinct from ⌘A (Select All in Edit) and ⌘⇧F
            // (future find). Maps the SwiftUI parity keybinding for
            // "toggle AI assistant."
            KeyEquivalentModifierMask = NSEventModifierMask.CommandKeyMask
                                       | NSEventModifierMask.ShiftKeyMask,
            // No Target set — AppKit's responder chain walks until
            // it finds a responder implementing the action; the
            // AppDelegate (us) handles it via the [Export] above.
            // validateMenuItem: below disables the item when no org
            // is selected.
        };
        viewMenu.AddItem(toggleChat);

        return new NSMenuItem("View") { Submenu = viewMenu };
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
