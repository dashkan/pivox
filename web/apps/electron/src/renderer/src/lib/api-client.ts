import { createPivoxApiClient } from '@pivox/features/api';

// Same env var the main process reads; falls back to the dev ngrok
// tunnel when unset. See env.d.ts + electron.vite.config.ts.
const BASE_URL = import.meta.env.VITE_BASE_URL || 'https://pivox.ngrok.app';

/**
 * Authenticated Pivox REST client for the Electron renderer. Points at
 * the Pivox app origin (the renderer's own `window.location.origin` is
 * a meaningless `file://` or vite-dev URL).
 */
export const apiClient = createPivoxApiClient({ baseUrl: BASE_URL });
