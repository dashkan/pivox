import { describe, expect, it } from 'vitest';

import { safeInternalPath } from '../../src/form-page/safe-internal-path';

const ORIGIN = 'https://app.pivox.dev';

describe('safeInternalPath — accepts same-app paths', () => {
  it('returns a plain leading-slash path unchanged', () => {
    expect(safeInternalPath('/connectors', ORIGIN)).toBe('/connectors');
  });

  it('preserves search and hash', () => {
    expect(safeInternalPath('/connectors?scope=main&q=x#top', ORIGIN)).toBe(
      '/connectors?scope=main&q=x#top',
    );
  });
});

describe('safeInternalPath — open-redirect rejections fall back to null', () => {
  it('rejects an absolute external URL', () => {
    expect(safeInternalPath('https://evil.com/steal', ORIGIN)).toBeNull();
  });

  it('rejects even an absolute SAME-origin URL (from is always a bare path)', () => {
    // The list only ever encodes pathname+search+hash, so a non-slash-leading
    // value — absolute URL or otherwise — is treated as untrusted and rejected.
    expect(safeInternalPath(`${ORIGIN}/connectors`, ORIGIN)).toBeNull();
  });

  it('rejects a protocol-relative URL', () => {
    expect(safeInternalPath('//evil.com', ORIGIN)).toBeNull();
  });

  it('rejects a backslash-normalization trick', () => {
    expect(safeInternalPath('/\\evil.com', ORIGIN)).toBeNull();
  });

  it('rejects an encoded-slash smuggling attempt', () => {
    expect(safeInternalPath('/%2f%2fevil.com', ORIGIN)).toBeNull();
    expect(safeInternalPath('/%5cevil.com', ORIGIN)).toBeNull();
  });

  it('rejects a value that does not start with a slash', () => {
    expect(safeInternalPath('connectors', ORIGIN)).toBeNull();
    expect(safeInternalPath('javascript:alert(1)', ORIGIN)).toBeNull();
  });

  it('rejects absent / empty input', () => {
    expect(safeInternalPath(undefined, ORIGIN)).toBeNull();
    expect(safeInternalPath(null, ORIGIN)).toBeNull();
    expect(safeInternalPath('', ORIGIN)).toBeNull();
  });
});
