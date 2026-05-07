import SwiftUI

/// Displays the official Google "G" logo from the asset catalog.
struct GoogleIcon: View {
  var size: CGFloat = 16

  var body: some View {
    Image("GoogleLogo")
      .resizable()
      .aspectRatio(contentMode: .fit)
      .frame(width: size, height: size)
  }
}
