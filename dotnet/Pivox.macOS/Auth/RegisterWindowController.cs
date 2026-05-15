using CoreGraphics;
using ObjCRuntime;
using Pivox.Shared.UI;

namespace Pivox.Auth;

/// <summary>
/// Hosts <see cref="RegisterViewController"/> in a window styled to
/// match <see cref="LoginWindowController"/>: titled + transparent
/// titlebar + full-size content + solid window background + same
/// MinSize. Visual family with Login.
/// </summary>
[Register("RegisterWindowController")]
public sealed class RegisterWindowController : NSWindowController
{
    public RegisterWindowController(RegisterViewController content)
        : base(BuildWindow())
    {
        // Controller-owned ContentViewController set after base() so
        // the responder chain wires through the controller. See
        // dotnet/CLAUDE.md Rule 17.
        ContentViewController = content;
    }

    private static NSWindow BuildWindow()
    {
        // Slightly taller than the Login window to comfortably fit
        // four fields + the social block + footer without crowding.
        var rect = new CGRect(0, 0, 560, 760);
        var style = NSWindowStyle.Titled
                  | NSWindowStyle.Closable
                  | NSWindowStyle.Miniaturizable
                  | NSWindowStyle.Resizable
                  | NSWindowStyle.FullSizeContentView;

        var window = new NSWindow(rect, style, NSBackingStore.Buffered, false)
        {
            Title = "Create your Pivox account",
            TitleVisibility = NSWindowTitleVisibility.Hidden,
            TitlebarAppearsTransparent = true,
            BackgroundColor = NSColor.WindowBackground,
            MovableByWindowBackground = true,
        };

        window.MinSize = new CGSize(
            ThemeMetrics.AuthCardWidth + 4 * ThemeMetrics.SpaceXl,
            640);

        window.Center();
        return window;
    }
}
