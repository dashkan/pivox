export interface TotpEnrollment {
  step: 'qr' | 'verify';
  qrUrl: string;
  secret: string;
}

export interface UserProfileState {
  displayName: string | null;
  email: string | null;
  photoURL: string | null;
  emailVerified: boolean;
  providers: Array<{
    providerId: string;
    displayName: string | null;
    email: string | null;
    photoURL: string | null;
  }>;
  availableProviders: Array<{ providerId: string; label: string }>;
  activePage: 'account' | 'security';
  error: string | null;
  success: string | null;
  mfaEnrolled: boolean;
  totpEnrollment: TotpEnrollment | null;
  /** Provider ID currently being linked, or null if no link flow is active. */
  linkingProvider: string | null;
}

export interface UserProfileActions {
  setActivePage: (page: 'account' | 'security') => void;
  clearStatus: () => void;

  // Profile
  updateDisplayName: (name: string) => Promise<void>;
  updatePhoto: (file: File) => Promise<void>;
  setPhotoURL: (url: string) => Promise<void>;
  removePhoto: () => Promise<void>;

  // Password
  setPassword: (newPassword: string) => Promise<void>;
  changePassword: (
    currentPassword: string,
    newPassword: string,
  ) => Promise<void>;

  // Email
  sendVerificationEmail: () => Promise<void>;
  changeEmail: (newEmail: string) => Promise<void>;

  // Providers
  linkProvider: (providerId: string) => Promise<void>;
  unlinkProvider: (providerId: string) => Promise<void>;
  /** Set the provider currently being linked (used by Electron override). */
  setLinkingProvider: (providerId: string | null) => void;

  // MFA
  startTotpEnrollment: () => Promise<void>;
  verifyTotpEnrollment: (code: string) => Promise<void>;
  cancelTotpEnrollment: () => void;
  unenrollTotp: () => Promise<void>;

  // Account
  deleteAccount: () => Promise<void>;

  // Sign out
  signOut: () => Promise<void>;
}

export interface UserProfileContextValue {
  state: UserProfileState;
  actions: UserProfileActions;
}
