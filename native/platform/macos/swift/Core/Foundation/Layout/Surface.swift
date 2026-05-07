import SwiftUI

/// Surface elevation tokens. Each level maps to a background style +
/// corner radius pair driven by the theme.
public enum SurfaceLevel {
    /// Page background — opaque, no corner radius.
    case base
    /// Subtle inline container — controlBackground with a small radius.
    case raised
    /// Inline elevated surface — `.ultraThinMaterial` with a medium
    /// radius, suitable for callouts within a page.
    case elevated
    /// Floating card — Liquid Glass on macOS 26+ (refracts content
    /// behind it), `.ultraThinMaterial` fallback otherwise. Used for
    /// auth forms and popovers where the surface reads as visually
    /// detached from its parent.
    case floating
}

struct SurfaceModifier: ViewModifier {
    let level: SurfaceLevel
    @Environment(\.pivoxTheme) private var theme

    @ViewBuilder
    func body(content: Content) -> some View {
        switch level {
        case .floating:
            // Liquid Glass refracts content behind the surface; the
            // material fallback is a flat translucent wash. Either
            // way, the corner radius is the floating-card token.
            //
            // Liquid Glass needs something interesting behind it
            // (gradient, image) to show its refraction; over a plain
            // window it looks indistinguishable from the fallback —
            // see LoginView's `authBackdrop` for the macOS 26 pattern.
            if #available(macOS 26, *) {
                content.glassEffect(
                    .regular,
                    in: .rect(cornerRadius: theme.radiusXL))
            } else {
                content.background(
                    .ultraThinMaterial,
                    in: RoundedRectangle(cornerRadius: theme.radiusXL))
            }
        default:
            content
                .background(backgroundForLevel)
                .clipShape(RoundedRectangle(cornerRadius: radiusForLevel))
        }
    }

    // Helpers below are only consulted from the `default:` branch of
    // `body(content:)`. `.floating` is routed separately to support
    // `glassEffect` on macOS 26+; its cases here are listed solely
    // for switch-exhaustiveness and document the fallback the
    // outer `#available(macOS 26, *) else` branch produces — keeping
    // them aligned means a future routing change won't silently drop
    // the floating-card visual contract.
    private var backgroundForLevel: some ShapeStyle {
        switch level {
        case .base:
            return AnyShapeStyle(theme.background)
        case .raised:
            return AnyShapeStyle(theme.backgroundRaised)
        case .elevated, .floating:
            return AnyShapeStyle(.ultraThinMaterial)
        }
    }

    private var radiusForLevel: CGFloat {
        switch level {
        case .base: return 0
        case .raised: return theme.radiusSM
        case .elevated: return theme.radiusMD
        case .floating: return theme.radiusXL
        }
    }
}

extension View {
    /// Applies a surface background at the given elevation level.
    public func surface(_ level: SurfaceLevel) -> some View {
        modifier(SurfaceModifier(level: level))
    }

    /// Conditional surface — passes `self` through unchanged when
    /// `enabled` is false. Used for auth screens' A/B toggle of the
    /// floating-card treatment without mutating the layout tree.
    @ViewBuilder
    public func surfaceIfEnabled(
        _ level: SurfaceLevel,
        when enabled: Bool
    ) -> some View {
        if enabled {
            self.surface(level)
        } else {
            self
        }
    }
}

#if DEBUG

/// All elevation levels in a single canvas so visual contrast is
/// obvious at a glance. `.floating` shows the material fallback
/// in Previews — Liquid Glass refraction needs a textured backdrop
/// to be visible (see LoginView's `authBackdrop`), which a flat
/// Preview canvas doesn't provide.
#Preview("All elevation levels") {
    VStack(spacing: 16) {
        sample("base — page background", .base)
        sample("raised — inline container", .raised)
        sample("elevated — callout / sheet", .elevated)
        sample("floating — auth card / popover", .floating)
    }
    .padding(20)
    .frame(width: 360)
}

@ViewBuilder
private func sample(_ label: String, _ level: SurfaceLevel) -> some View {
    Text(label)
        .font(.system(.body, design: .default))
        .foregroundStyle(.primary)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .surface(level)
}

#endif
