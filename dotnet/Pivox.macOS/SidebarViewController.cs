using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox;

/// <summary>
/// Sidebar pane for the main split view. View hierarchy is built
/// in code (no storyboard scene, no xib).
/// </summary>
[Register("SidebarViewController")]
public sealed class SidebarViewController : NSViewController
{
	public SidebarViewController() : base((string?)null, null)
	{
	}

	public override void LoadView()
	{
		// NSViewController's default LoadView tries to load from a
		// nib of the same name. For code-only VCs we provide the
		// view ourselves to short-circuit that lookup.
		View = new NSView(new CGRect(0, 0, 200, 400));
	}

	public override void ViewDidLoad()
	{
		base.ViewDidLoad();

		var label = new NSTextField
		{
			StringValue = "Sidebar",
			Editable = false,
			Bordered = false,
			DrawsBackground = false,
			Selectable = false,
			Alignment = NSTextAlignment.Center,
			Font = ThemeFonts.NS(ThemeFont.Body),
			TranslatesAutoresizingMaskIntoConstraints = false,
		};

		View.AddSubview(label);

		NSLayoutConstraint.ActivateConstraints(new[]
		{
			label.CenterXAnchor.ConstraintEqualTo(View.CenterXAnchor),
			label.CenterYAnchor.ConstraintEqualTo(View.CenterYAnchor),
		});
	}
}
