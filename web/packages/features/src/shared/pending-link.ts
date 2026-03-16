import type { AuthCredential } from "firebase/auth"

export interface PendingLink {
  email: string
  credential: AuthCredential
  providerName: string
}

let pendingLink: PendingLink | null = null

export function setPendingLink(link: PendingLink) {
  pendingLink = link
}

export function getPendingLink(): PendingLink | null {
  return pendingLink
}

export function clearPendingLink() {
  pendingLink = null
}
