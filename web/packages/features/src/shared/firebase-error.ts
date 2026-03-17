export function firebaseErrorMessage(e: unknown): string {
  if (typeof e === 'object' && e !== null && 'code' in e) {
    const code = (e as { code: string }).code;
    switch (code) {
      case 'auth/invalid-email':
        return 'Invalid email address';
      case 'auth/user-disabled':
        return 'This account has been disabled';
      case 'auth/user-not-found':
        return 'No account found with this email';
      case 'auth/wrong-password':
        return 'Incorrect password';
      case 'auth/invalid-credential':
        return 'Invalid email or password';
      case 'auth/email-already-in-use':
        return 'This email is already registered';
      case 'auth/weak-password':
        return 'Password must be at least 6 characters';
      case 'auth/popup-closed-by-user':
        return 'Sign-in popup was closed';
      case 'auth/popup-blocked':
        return 'Sign-in popup was blocked. Please allow popups';
      case 'auth/expired-action-code':
        return 'This link has expired. Please request a new one';
      case 'auth/invalid-action-code':
        return 'This link is invalid or has already been used';
      case 'auth/requires-recent-login':
        return 'Please sign in again to complete this action';
      case 'auth/second-factor-already-enrolled':
        return 'An authenticator app is already enrolled';
      case 'auth/unsupported-first-factor':
        return 'Please verify your email before enabling two-factor authentication';
      case 'auth/unverified-email':
        return 'Please verify your email before enabling two-factor authentication';
      case 'auth/operation-not-allowed':
        return 'This feature is not enabled. Please contact support';
      default:
        return 'Something went wrong. Please try again';
    }
  }
  return 'Something went wrong. Please try again';
}
