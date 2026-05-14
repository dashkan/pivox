using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Client;
using Pivox.Shared.Auth;
using Pivox.Auth;

namespace Pivox;

/// <summary>
/// Builds the entire app UI in code. No storyboard, no xib.
///
/// Layout:
///   NSWindow (titled, closable, resizable)
///     └─ NSSplitViewController (contentViewController)
///          ├─ Sidebar pane (SidebarViewController)
///          └─ Content pane (DetailViewController)
///
/// Main menu is constructed in BuildMainMenu(). Minimum-viable
/// shape: Application menu (Quit) + Edit menu (Cut/Copy/Paste/
/// Select All) so NSTextField shortcuts work. Add File/View/Window
/// menus as the app grows.
/// </summary>
[Register("AppDelegate")]
public sealed class AppDelegate : NSApplicationDelegate
{
	// Strong refs so window, controllers, and services don't get
	// GC'd while the app is running.
	private NSWindow? _window;
	private NSSplitViewController? _splitVC;
	private IAuthService? _auth;
	private PivoxClient? _pivox;

	public override void DidFinishLaunching(NSNotification notification)
	{
		NSApplication.SharedApplication.MainMenu = BuildMainMenu();

		// Single auth service for the process — wraps FIRAuth + Google
		// OAuth. Passed wherever auth is needed.
		_auth = new MacOsAuthService();

		// Single gRPC client. Auto-attaches Bearer tokens via the
		// AuthInterceptor. Endpoint resolves from CloudConfig (defaults
		// to pivox.ngrok.app; overridable via PIVOX_GRPC_HOST).
		_pivox = new PivoxClient(_auth);

		var sidebar = new SidebarViewController();
		var detail = new DetailViewController(_auth, _pivox);

		_splitVC = new NSSplitViewController();
		_splitVC.AddSplitViewItem(NSSplitViewItem.CreateSidebar(sidebar));
		_splitVC.AddSplitViewItem(new NSSplitViewItem { ViewController = detail });

		var styleMask = NSWindowStyle.Titled
			| NSWindowStyle.Closable
			| NSWindowStyle.Miniaturizable
			| NSWindowStyle.Resizable;

		_window = new NSWindow(
			new CGRect(0, 0, 900, 600),
			styleMask,
			NSBackingStore.Buffered,
			false)
		{
			Title = "Pivox",
			ContentViewController = _splitVC,
		};
		_window.Center();
		_window.MakeKeyAndOrderFront(null);

		NSApplication.SharedApplication.ActivationPolicy = NSApplicationActivationPolicy.Regular;
		NSApplication.SharedApplication.Activate();
	}

	public override bool ApplicationShouldTerminateAfterLastWindowClosed(NSApplication sender) => true;

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
		// The first menu item's title is shown as the app's
		// application menu label, taken from the bundle name.
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

		// Telling NSApp this is the Window menu lets AppKit
		// auto-populate it with open windows.
		NSApplication.SharedApplication.WindowsMenu = windowMenu;

		return new NSMenuItem("Window") { Submenu = windowMenu };
	}
}
