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

  // Profile
  updateDisplayName: (name: string) => Promise<void>
  updatePhoto: (file: File) => Promise<void>
  removePhoto: () => Promise<void>

  // Password
  changePassword: (currentPassword: string, newPassword: string) => Promise<void>

  // Email
  // TODO: addEmail, removeEmail, setPrimaryEmail, sendVerification

  // Providers
  // TODO: linkProvider, unlinkProvider

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
