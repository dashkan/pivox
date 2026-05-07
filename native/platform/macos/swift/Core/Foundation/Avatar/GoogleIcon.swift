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

#if DEBUG
#Preview("Sizes") {
    HStack(alignment: .center, spacing: 16) {
        GoogleIcon(size: 16)
        GoogleIcon(size: 24)
        GoogleIcon(size: 32)
        GoogleIcon(size: 48)
    }
    .padding()
}
#endif
