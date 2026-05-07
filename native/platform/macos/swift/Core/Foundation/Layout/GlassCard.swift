import SwiftUI

extension View {
  /// Applies a glass card background — Liquid Glass on macOS 26+,
  /// ultraThinMaterial on older versions. Only used for floating cards
  /// (auth forms, popovers) where the glass effect adds depth.
  @ViewBuilder
  func glassCard() -> some View {
    if #available(macOS 26, *) {
      self.glassEffect(.regular, in: .rect(cornerRadius: 16))
    } else {
      self.background(
        .ultraThinMaterial,
        in: RoundedRectangle(cornerRadius: 16)
      )
    }
  }

  /// Conditional glass card — passes `self` through unchanged when
  /// `enabled` is false. Used for the auth screens so we can A/B
  /// compare with/without the floating-card treatment at runtime.
  @ViewBuilder
  func glassCardIfEnabled(_ enabled: Bool) -> some View {
    if enabled { self.glassCard() } else { self }
  }
}
