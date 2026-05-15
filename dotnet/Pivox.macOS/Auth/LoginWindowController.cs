using CoreGraphics;
using ObjCRuntime;
using Pivox.Shared.UI;

namespace Pivox.Auth;

/// <summary>
/// Hosts <see cref="LoginViewController"/> in a window styled to match
/// the new macOS 26 design: solid window background with the Liquid
/// Glass auth card floating on top.
///
/// Per WWDC 2025 session 310 ("Build an AppKit app with the new design"),
/// glass is for top-level UI elements that float above content — not
/// for the window background itself. So:
/// <list type="bullet">
/// <item><b>Window content</b> — plain <see cref="NSView"/> with
///   <see cref="NSColor.WindowBackground"/>. Solid surface that the
///   glass card has something definite to refract from.</item>
/// <item><b>Auth card</b> — <c>NSGlassEffectView</c> (set up inside
///   <see cref="LoginViewController.BuildCard"/>). Proper Liquid Glass
///   — adaptive, light-reflecting, the right primitive for floating
///   top-level UI elements like the login form.</item>
/// </list>
///
/// Window chrome: titled + transparent titlebar +
/// <see cref="NSWindowStyle.FullSizeContentView"/> so the content view
/// extends under the title bar and the traffic-light buttons hover
/// over the surface.
/// </summary>
[Register("LoginWindowController")]
public sealed class LoginWindowController : NSWindowController
{
    public LoginWindowController(LoginViewController content)
        : base(BuildWindow())
    {
        // Set ContentViewController on the CONTROLLER (not the window
        // inside BuildWindow). The controller-owned path is what wires
        // the responder chain (controller ↔ window ↔ contentVC) — set
        // it on the window before the controller exists and you get a
        // contentView with broken responder integration: window may
        // show without responding to events, or not appear at all.
        ContentViewController = content;
    }

    private static NSWindow BuildWindow()
    {
        // Sized for a centered auth card with comfortable margins on a
        // standard 1440-wide display. Resizable so users with smaller or
        // larger displays can adjust; the card itself stays at 360pt
        // wide regardless.
        var rect = new CGRect(0, 0, 560, 680);
        var style = NSWindowStyle.Titled
                  | NSWindowStyle.Closable
                  | NSWindowStyle.Miniaturizable
                  | NSWindowStyle.Resizable
                  | NSWindowStyle.FullSizeContentView;

        var window = new NSWindow(rect, style, NSBackingStore.Buffered, false)
        {
            Title = "Sign in to Pivox",
            TitleVisibility = NSWindowTitleVisibility.Hidden,
            TitlebarAppearsTransparent = true,
            // Solid window background — appearance-aware, matches the
            // rest of the system (System Settings, Finder, etc.). The
            // glass card on top is the only translucent element.
            BackgroundColor = NSColor.WindowBackground,
            MovableByWindowBackground = true,
        };

        // ContentViewController is set by the LoginWindowController
        // ctor body — AFTER the window is bound to a controller —
        // so the responder chain wires up correctly. Setting it here
        // on a window with no associated controller silently breaks
        // event integration and (empirically) the window doesn't even
        // become visible.

        // Minimum size: ensure the card always has comfortable
        // breathing room. Mirrors the SwiftUI version's
        // .frame(minWidth:, minHeight:) on the auth view. Card is
        // AuthCardWidth wide, plus generous horizontal margins; height
        // accommodates the form, social button, and title bar without
        // clipping.
        window.MinSize = new CGSize(
            ThemeMetrics.AuthCardWidth + 4 * ThemeMetrics.SpaceXl, // card + ~64pt margins both sides
            560);                                                    // header + form + sep + social + chrome

        window.Center();
        return window;
    }
}
