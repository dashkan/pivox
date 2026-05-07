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

    private var backgroundForLevel: some ShapeStyle {
        switch level {
        case .base:
            return AnyShapeStyle(theme.background)
        case .raised:
            return AnyShapeStyle(theme.backgroundRaised)
        case .elevated:
            return AnyShapeStyle(.ultraThinMaterial)
        case .floating:
            // Handled above with `#available` to support Liquid Glass.
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
