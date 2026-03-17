export interface UserProfileState {
  displayName: string | null
  email: string | null
  photoURL: string | null
  emailVerified: boolean
  providers: Array<{
    providerId: string
    displayName: string | null
    email: string | null
    photoURL: string | null
  }>
  activePage: "account" | "security"
  error: string | null
  success: string | null
}

export interface UserProfileActions {
  setActivePage: (page: "account" | "security") => void
  clearStatus: () => void

  // Profile
  updateDisplayName: (name: string) => Promise<void>
  updatePhoto: (file: File) => Promise<void>
  removePhoto: () => Promise<void>

  // Password
  setPassword: (newPassword: string) => Promise<void>
  changePassword: (currentPassword: string, newPassword: string) => Promise<void>

  // Email
  sendVerificationEmail: () => Promise<void>
  // TODO: addEmail, removeEmail, setPrimaryEmail

  // Providers
  unlinkProvider: (providerId: string) => Promise<void>
  // TODO: linkProvider

  // MFA
  // TODO: enrollMfa, unenrollMfa

  // Sessions
  // TODO: listSessions, revokeSession, revokeAllSessions

  // Account
  deleteAccount: () => Promise<void>

  // Sign out
  signOut: () => Promise<void>
}

export interface UserProfileContextValue {
  state: UserProfileState
  actions: UserProfileActions
}
