export interface ForgotPasswordState {
  email: string
  error: string | null
  success: boolean
}

export interface ForgotPasswordActions {
  updateEmail: (email: string) => void
  formAction: (payload: FormData) => void
}

export interface ForgotPasswordMeta {
  emailRef: React.RefObject<HTMLInputElement | null>
}

export interface ForgotPasswordContextValue {
  state: ForgotPasswordState
  actions: ForgotPasswordActions
  meta: ForgotPasswordMeta
}
