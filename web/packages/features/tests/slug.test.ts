import { describe, expect, it } from 'vitest';

import { isValidSlug, slugify } from '@/create-org/slug';

describe('slugify', () => {
  it('lowercases letters and digits and preserves them', () => {
    expect(slugify('Acme123')).toBe('acme123');
  });

  it('collapses whitespace and underscores into single hyphens', () => {
    expect(slugify('Acme   Co_Ltd')).toBe('acme-co-ltd');
  });

  it('collapses runs of hyphens', () => {
    expect(slugify('acme---co')).toBe('acme-co');
  });

  it('trims trailing hyphens', () => {
    expect(slugify('Acme Inc.   ')).toBe('acme-inc');
  });

  it('does not emit a leading hyphen for a leading separator', () => {
    // The first non-alphanumeric char must NOT produce a leading
    // hyphen — server validation rejects slugs that don't start
    // with a letter.
    expect(slugify('   Acme')).toBe('acme');
  });

  it('drops emoji and accented characters', () => {
    expect(slugify('Café 🌟 Acme')).toBe('caf-acme');
  });

  it('truncates to 20 characters', () => {
    expect(
      slugify('a-very-long-organization-name-here').length,
    ).toBeLessThanOrEqual(20);
  });
});

describe('isValidSlug', () => {
  it('accepts the canonical shape', () => {
    expect(isValidSlug('acme')).toBe(true);
    expect(isValidSlug('acme-corp-2024')).toBe(true);
  });

  it('rejects slugs shorter than 4 characters', () => {
    expect(isValidSlug('abc')).toBe(false);
  });

  it('rejects slugs longer than 20 characters', () => {
    expect(isValidSlug('a23456789012345678901')).toBe(false);
  });

  it('rejects slugs that do not start with a letter', () => {
    expect(isValidSlug('1acme')).toBe(false);
    expect(isValidSlug('-acme')).toBe(false);
  });

  it('rejects uppercase letters', () => {
    expect(isValidSlug('Acme')).toBe(false);
  });

  it('rejects forbidden punctuation', () => {
    expect(isValidSlug('acme_co')).toBe(false);
    expect(isValidSlug('acme.co')).toBe(false);
    expect(isValidSlug('acme co')).toBe(false);
  });
});
