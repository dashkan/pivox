import { chmodSync, existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { app, safeStorage } from 'electron';

import type { TokenPersistence } from './auth-session';

/**
 * True only when safeStorage will actually encrypt. On Linux
 * `isEncryptionAvailable()` can return true while the backend is `basic_text`
 * (no keyring) — a hardcoded key, i.e. obfuscation, not encryption. Refuse that:
 * re-authenticating on next launch is a better failure than a near-plaintext
 * token on disk. macOS Keychain / Windows DPAPI are always real.
 */
function encryptionUsable(): boolean {
  if (!safeStorage.isEncryptionAvailable()) return false;
  if (process.platform === 'linux') {
    try {
      if (safeStorage.getSelectedStorageBackend() === 'basic_text') return false;
    } catch {
      // Older Electron without getSelectedStorageBackend — fall through.
    }
  }
  return true;
}

/**
 * safeStorage-backed {@link TokenPersistence} for the refresh token.
 *
 * Only the refresh token is persisted (the access token stays in memory). It's
 * encrypted with Electron's safeStorage — OS keychain-backed (Keychain / DPAPI /
 * libsecret) — and written to a 0600 file under userData. When OS encryption is
 * unavailable we decline to persist rather than write plaintext: the cost is
 * re-authenticating on next launch, not a leaked token.
 *
 * This is the electron-coupled adapter behind AuthSession's injected persistence
 * seam; the engine itself has no electron import and is unit-tested with a fake.
 */
export function createTokenStore(): TokenPersistence {
  const file = join(app.getPath('userData'), 'auth.bin');

  return {
    load() {
      if (!existsSync(file) || !encryptionUsable()) return null;
      try {
        return safeStorage.decryptString(readFileSync(file));
      } catch {
        // Corrupt / undecryptable (e.g. keychain rotated) — treat as no session.
        return null;
      }
    },

    save(refreshToken) {
      if (!encryptionUsable()) return;
      writeFileSync(file, safeStorage.encryptString(refreshToken), { mode: 0o600 });
      // `mode` only applies on creation; enforce 0600 on overwrite too, so a
      // pre-existing looser-permissioned file gets tightened.
      try {
        chmodSync(file, 0o600);
      } catch {
        // Best-effort on platforms/filesystems without POSIX modes (Windows).
      }
    },

    clear() {
      rmSync(file, { force: true });
    },
  };
}
