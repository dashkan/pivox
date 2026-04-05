import SwiftUI

struct ProfileView: View {
    var onSignOut: () -> Void

    var body: some View {
        VStack(spacing: 20) {
            // Avatar
            Image(systemName: "person.circle.fill")
                .resizable()
                .frame(width: 64, height: 64)
                .foregroundStyle(.secondary)

            // User info (placeholder data)
            VStack(spacing: 4) {
                Text("Ashkan Daie")
                    .font(.headline)
                Text("ashkan@pivox.app")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            Divider()

            // Actions
            VStack(spacing: 8) {
                Button(action: { /* placeholder */ }) {
                    HStack {
                        Image(systemName: "gear")
                        Text("Account Settings")
                        Spacer()
                    }
                }
                .buttonStyle(.plain)

                Button(action: onSignOut) {
                    HStack {
                        Image(systemName: "rectangle.portrait.and.arrow.forward")
                        Text("Sign Out")
                        Spacer()
                    }
                    .foregroundStyle(.red)
                }
                .buttonStyle(.plain)
            }
        }
        .padding()
    }
}
