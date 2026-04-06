import SwiftUI

struct ProfileView: View {
    private var auth = AuthService.shared

    var body: some View {
        VStack(spacing: 20) {
            // Avatar
            Image(systemName: "person.circle.fill")
                .resizable()
                .frame(width: 64, height: 64)
                .foregroundStyle(.secondary)

            // User info
            VStack(spacing: 4) {
                Text(auth.currentUser?.displayName ?? "User")
                    .font(.headline)
                    .accessibilityIdentifier("profile-display-name")
                Text(auth.currentUser?.email ?? "")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("profile-email")
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

                Button(action: { auth.signOut() }) {
                    HStack {
                        Image(systemName: "rectangle.portrait.and.arrow.forward")
                        Text("Sign Out")
                        Spacer()
                    }
                    .foregroundStyle(.red)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("profile-sign-out")
            }
        }
        .padding()
    }
}
