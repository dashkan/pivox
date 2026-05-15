namespace Pivox.Shared.Navigation;

/// <summary>
/// Closed hierarchy of every route the app can show. Records, so equality
/// is value-based — <c>route1 == route2</c> means "same screen with same
/// args," which is what <see cref="AppRouter"/>'s history comparisons rely
/// on.
///
/// Adding a route: declare a new <c>sealed record</c> nested under
/// <see cref="AppRoute"/>. Carrying args (org id, asset id, etc.) is just
/// record positional/property syntax. No platform-specific routes — those
/// belong in the platform layer's window/view-controller mapping.
///
/// Phase 1 routes (Login, Shell) are the only two needed before in-shell
/// navigation lands. <c>Shell</c> stays a single route until we have real
/// sub-screens (Dashboard, Settings, OrgPicker, …) at which point those
/// become <c>Shell.Dashboard</c> etc. and gain Push/Pop back-history
/// semantics. Both platforms' window/page observers branch on the route's
/// concrete type, so adding cases is additive.
/// </summary>
public abstract record AppRoute
{
    private AppRoute() { }

    /// <summary>Pre-auth route. Login form. No back history (signing out
    /// wipes history; you can't navigate back into a signed-in shell).</summary>
    public sealed record Login : AppRoute;

    /// <summary>Post-auth app shell — the sidebar+detail split. Currently
    /// the only post-auth route. As in-shell screens land, this becomes the
    /// root of a Push/Pop nav stack.</summary>
    public sealed record Shell : AppRoute;
}
