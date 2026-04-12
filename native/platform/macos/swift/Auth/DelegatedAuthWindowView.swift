import SwiftUI

/// Window content for a delegated-auth sign-in flow.
///
/// Reuses the existing `LoginView`, but injected with an `AuthService`
/// bound to an isolated, named Firebase app. Registration isn't offered
/// here — the delegated flow is strictly "sign in so the plugin can get
/// a custom token", not "create an account from a plugin".
struct DelegatedAuthWindowView: View {
  var auth: AuthService

  var body: some View {
    VStack(spacing: 0) {
      LoginView(auth: auth, onSwitchToRegister: {})
    }
    .frame(minWidth: 480, minHeight: 560)
  }
}
