import SwiftUI

/// Surface elevation levels for AIElements containers.
public enum SurfaceLevel {
    case base
    case raised
    case elevated
    case floating
}

/// Applies a surface background appropriate to the elevation level.
/// Elevated and floating use Liquid Glass on macOS 26+, material fallback otherwise.
struct SurfaceModifier: ViewModifier {
    let level: SurfaceLevel
    @Environment(\.pivoxTheme) private var theme

    func body(content: Content) -> some View {
        content
            .background(backgroundForLevel)
            .clipShape(RoundedRectangle(cornerRadius: radiusForLevel))
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
            return AnyShapeStyle(.thinMaterial)
        }
    }

    private var radiusForLevel: CGFloat {
        switch level {
        case .base: return 0
        case .raised: return theme.radiusSM
        case .elevated: return theme.radiusMD
        case .floating: return theme.radiusLG
        }
    }
}

extension View {
    /// Applies a surface background at the given elevation level.
    public func surface(_ level: SurfaceLevel) -> some View {
        modifier(SurfaceModifier(level: level))
    }
}
