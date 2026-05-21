import { reportError } from './report';

/**
 * Install global error listeners. Call once, client-side, at app startup.
 *
 * Covers the two browser channels for errors nothing else caught:
 *   - `unhandledrejection` — a rejected promise with no `.catch`
 *   - `error`              — a synchronous uncaught error
 *
 * No-op under SSR (no `window`). Server-side unhandled rejections are a
 * separate channel (`process.on('unhandledRejection', …)`) and not this
 * function's concern.
 *
 * Deliberately does NOT call `event.preventDefault()` — the browser's
 * native console entry (with a clickable, source-mapped stack) stays
 * useful in dev; `reportError` is additive.
 */
export function installErrorReporters(): void {
  if (typeof window === 'undefined') return;

  window.addEventListener('unhandledrejection', (event) => {
    reportError(event.reason, { source: 'unhandledrejection' });
  });

  window.addEventListener('error', (event) => {
    reportError(event.error ?? event.message, { source: 'error' });
  });
}
