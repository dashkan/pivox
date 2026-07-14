import { describe, expect, it } from 'vitest';

import { decideNavigation } from '../src/main/navigation-guard';

// The renderer's own origin: the Vite dev server in `forge start`, and `null`
// in a packaged build (the renderer is loaded from file:// via win.loadFile,
// and a file:// document has an opaque origin).
const DEV_ORIGIN = 'http://localhost:5173';

describe('decideNavigation', () => {
  describe('dev (renderer served from the Vite dev server)', () => {
    it('allows navigation within the renderer origin', () => {
      expect(decideNavigation('http://localhost:5173/', DEV_ORIGIN)).toBe(
        'allow',
      );
      expect(
        decideNavigation('http://localhost:5173/#/auth/login', DEV_ORIGIN),
      ).toBe('allow');
    });

    it('does not treat a different port as the same origin', () => {
      // A stray process on another localhost port is NOT us.
      expect(decideNavigation('http://localhost:9999/', DEV_ORIGIN)).toBe(
        'open-external',
      );
    });

    it('does not confuse a lookalike host with the renderer origin', () => {
      expect(
        decideNavigation('http://localhost.evil.com/', DEV_ORIGIN),
      ).toBe('open-external');
    });
  });

  describe('packaged (renderer loaded from file://)', () => {
    it('allows navigation within the bundled app', () => {
      expect(decideNavigation('file:///Applications/Pivox.app/index.html', null)).toBe(
        'allow',
      );
    });

    it('does not allow file:// once a dev server origin is in play', () => {
      // In dev the renderer is http(s); a file:// navigation is not the app.
      expect(decideNavigation('file:///etc/passwd', DEV_ORIGIN)).toBe('deny');
    });
  });

  describe('external navigation', () => {
    it('sends http(s) to the system browser rather than the app window', () => {
      // THE ATTACK THIS EXISTS FOR: a compromised renderer setting
      // location.href to an attacker page would otherwise navigate the
      // top-level frame out of the bundled origin while KEEPING the preload
      // bridge — handing the attacker window.api, including
      // auth:get-access-token (a live access token).
      expect(decideNavigation('https://evil.example/steal', DEV_ORIGIN)).toBe(
        'open-external',
      );
      expect(decideNavigation('https://evil.example/steal', null)).toBe(
        'open-external',
      );
      expect(decideNavigation('http://evil.example/', null)).toBe(
        'open-external',
      );
    });
  });

  describe('everything else is denied', () => {
    it('denies non-http(s) schemes rather than handing them to the OS', () => {
      // shell.openExternal on these would launch another local app / handler.
      for (const url of [
        'javascript:alert(1)',
        'data:text/html,<script>alert(1)</script>',
        'smb://attacker/share',
        'file:///etc/passwd', // packaged-origin case covered above; here dev
        'pivox://oidc-callback', // our own scheme is for deep links, not navigation
      ]) {
        expect(decideNavigation(url, DEV_ORIGIN)).toBe('deny');
      }
    });

    it('denies an unparseable url', () => {
      expect(decideNavigation('not a url', DEV_ORIGIN)).toBe('deny');
      expect(decideNavigation('', null)).toBe('deny');
    });

    it('denies our own custom scheme even in a packaged build', () => {
      expect(decideNavigation('pivox://oidc-callback', null)).toBe('deny');
    });
  });
});
