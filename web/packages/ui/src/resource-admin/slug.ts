/**
 * Slugify a human-readable name into a resource identifier:
 *   - lowercased
 *   - letters and digits preserved
 *   - whitespace / `-` / `_` collapsed to a single `-`
 *   - leading/trailing `-` trimmed
 *   - truncated to 63 characters
 *
 * Mirrors `create-org`'s `slugify`; used to auto-derive `connector_id` /
 * `secret_id` from the display name on the create forms.
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
  return out.slice(0, 63);
}

/**
 * A valid resource identifier for a connector/secret: starts with a lowercase
 * letter, then lowercase letters, digits, or hyphens (no trailing hyphen). The
 * server requires the id to be non-empty; this additionally keeps it URL-safe
 * as a resource-name path segment.
 */
export function isValidIdentifier(id: string): boolean {
  return /^[a-z]([a-z0-9-]*[a-z0-9])?$/.test(id);
}
