import type { AuthCredential } from 'firebase/auth';

export interface PendingLink {
  email: string;
  credential: AuthCredential;
  providerName: string;
}

let pendingLink: PendingLink | null = null;
let pendingLinkTimeout: ReturnType<typeof setTimeout> | null = null;

// AUTHN-09: Auto-clear after 5 minutes to minimize credential lifetime.
const PENDING_LINK_TTL_MS = 5 * 60 * 1000;

export function setPendingLink(link: PendingLink) {
  clearPendingLink();
  pendingLink = link;
  pendingLinkTimeout = setTimeout(clearPendingLink, PENDING_LINK_TTL_MS);
}

export function getPendingLink(): PendingLink | null {
  return pendingLink;
}

export function clearPendingLink() {
  pendingLink = null;
  if (pendingLinkTimeout) {
    clearTimeout(pendingLinkTimeout);
    pendingLinkTimeout = null;
  }
}
