/**
 * Slugify a human-readable name into a server-acceptable
 * organizationId:
 *   - lowercased
 *   - letters and digits preserved
 *   - whitespace / `-` / `_` collapsed to a single `-`
 *   - leading/trailing `-` trimmed
 *   - truncated to 20 characters (server cap)
 *
 * Mirrors the SwiftUI native `CreateOrgView.slugify` so the UX is
 * identical across platforms. The server's buf-validate rule is
 * `^[a-z][a-z0-9-]{3,19}$`; the caller validates the final string
 * via {@link isValidSlug} before submitting.
 */
export function slugify(name: string): string {
  const lower = name.toLowerCase();
  let out = '';
  let lastHyphen = false;
  for (const ch of lower) {
    if (/[a-z0-9]/.test(ch)) {
      out += ch;
      lastHyphen = false;
    } else if (/[\s_-]/.test(ch)) {
      if (!lastHyphen && out.length > 0) {
        out += '-';
        lastHyphen = true;
      }
    }
    // Anything else is dropped (emoji, punctuation, accented letters).
  }
  while (out.endsWith('-')) out = out.slice(0, -1);
  return out.slice(0, 20);
}

/**
 * Mirrors the server's buf-validate rule for `organization_id`:
 *   ^[a-z][a-z0-9-]{3,19}$ — 4-20 chars, must start with a letter,
 *   lowercase letters/digits/hyphens only.
 *
 * Keeping the predicate local stops the submit button from firing on
 * a guaranteed-InvalidArgument input.
 */
export function isValidSlug(slug: string): boolean {
  return /^[a-z][a-z0-9-]{3,19}$/.test(slug);
}
