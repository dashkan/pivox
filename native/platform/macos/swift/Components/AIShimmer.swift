import SwiftUI

/// Apple-Intelligence-style animated gradient border. Continuously
/// rotates an `AngularGradient` stroke around the given shape using
/// a `TimelineView(.animation)` phase driven by the system clock —
/// no `@State` churn, no redundant view updates, and the animation
/// stays in sync even when the view re-appears after being offscreen.
///
/// macOS 26 doesn't expose the Writing Tools / Siri shimmer for
/// third-party inputs; every AI app (Claude, Arc, Notion) builds
/// their own. This is ours.
///
/// ## Parameters
/// - `shape`: any `InsettableShape` (Capsule, RoundedRectangle, etc.)
/// - `intensity`: 0..1. Scales both the line width and opacity so the
///   shimmer can fade from ambient (0.3–0.4) to prominent (1.0) based
///   on focus, streaming, or hover state.
/// - `lineWidth`: base stroke width before intensity scaling.
/// - `periodSeconds`: how long one full rotation takes. Lower = faster
///   shimmer. Default 6s is calm enough to not feel restless.
///
/// ## Color palette
/// Pastel rainbow that approximates Apple's Writing Tools treatment.
/// Same palette in light + dark — the underlying material absorbs
/// enough luminance to keep the colors readable in both modes.
public struct AIShimmer<S: InsettableShape>: ViewModifier {
    let shape: S
    let intensity: Double
    let lineWidth: CGFloat
    let periodSeconds: Double

    public func body(content: Content) -> some View {
        content.overlay {
            TimelineView(.animation) { context in
                // Map elapsed time → rotation angle. `truncatingRemainder`
                // keeps the number bounded so double-precision math
                // doesn't drift over long sessions.
                let t = context.date.timeIntervalSinceReferenceDate
                let phase = (t.truncatingRemainder(dividingBy: periodSeconds) / periodSeconds) * 360.0

                shape
                    .strokeBorder(
                        AngularGradient(
                            colors: Self.palette,
                            center: .center,
                            startAngle: .degrees(phase),
                            endAngle: .degrees(phase + 360)
                        ),
                        lineWidth: lineWidth * intensity
                    )
                    .opacity(intensity)
            }
            // Don't let the shimmer eat clicks on the view behind it.
            .allowsHitTesting(false)
        }
    }

    private static var palette: [Color] { AIShimmerPalette.stops }
}

extension View {
    /// Adds an animated iridescent border in the shape of your
    /// choice. Pairs with a material or `Color.clear` background;
    /// the shimmer is purely an overlay.
    public func aiShimmer<S: InsettableShape>(
        shape: S,
        intensity: Double = 1.0,
        lineWidth: CGFloat = 2.0,
        periodSeconds: Double = 6.0
    ) -> some View {
        modifier(AIShimmer(
            shape: shape,
            intensity: intensity,
            lineWidth: lineWidth,
            periodSeconds: periodSeconds))
    }

    /// Fills an SF Symbol (or any recolorable content) with the same
    /// animated angular gradient, rotated in place. Use on the icon
    /// itself for an AI-flavored glyph; use `aiShimmer(shape:)` for
    /// a ring around a whole field or button.
    ///
    /// When `isActive` is false this is a no-op — the view renders
    /// with its inherited foreground color. When true, the foreground
    /// is replaced with a continuously-rotating AngularGradient.
    /// TimelineView only runs while active, so there's no
    /// background animation cost at rest.
    public func aiShimmerSymbol(
        isActive: Bool,
        periodSeconds: Double = 3.0
    ) -> some View {
        modifier(AIShimmerSymbol(
            isActive: isActive,
            periodSeconds: periodSeconds))
    }
}

/// Internal — see `View.aiShimmerSymbol`.
public struct AIShimmerSymbol: ViewModifier {
    let isActive: Bool
    let periodSeconds: Double

    public func body(content: Content) -> some View {
        if isActive {
            TimelineView(.animation) { context in
                let t = context.date.timeIntervalSinceReferenceDate
                let phase = (t.truncatingRemainder(dividingBy: periodSeconds) / periodSeconds) * 360.0
                content
                    .foregroundStyle(
                        AngularGradient(
                            colors: AIShimmerPalette.stops,
                            center: .center,
                            startAngle: .degrees(phase),
                            endAngle: .degrees(phase + 360)))
            }
        } else {
            content
        }
    }
}

/// Shared color stops used by both the border and the symbol
/// shimmer so the two read as one visual language.
enum AIShimmerPalette {
    static let stops: [Color] = [
        Color(red: 1.00, green: 0.50, blue: 0.70),   // pink
        Color(red: 1.00, green: 0.68, blue: 0.50),   // coral
        Color(red: 1.00, green: 0.85, blue: 0.45),   // gold
        Color(red: 0.58, green: 0.88, blue: 1.00),   // sky
        Color(red: 0.72, green: 0.60, blue: 0.96),   // lavender
        Color(red: 1.00, green: 0.50, blue: 0.70),   // pink (seamless wrap)
    ]
}
