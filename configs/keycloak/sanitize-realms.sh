#!/usr/bin/env bash
#
# sanitize-realms.sh — turn raw Keycloak realm exports into committable,
# secret-free baselines.
#
# Re-export the realms into this directory (pivox-realm.json, acme-realm.json),
# then run this script. In place, per realm, it:
#
#   1. Strips the realm key-provider components — Keycloak regenerates fresh
#      signing/encryption keys on import, so no key material lands in the repo.
#   2. Drops dynamically-registered (DCR) clients — MCP clients (Claude Code,
#      VS Code, ...) register ephemeral OAuth clients via DCR, each with a UUID
#      clientId. They are per-install throwaways created on demand at connect
#      time, NOT part of the committable realm baseline; leaving them in would
#      recreate stale registrations on every fresh --import-realm. Matched by the
#      UUID-shaped clientId (no static client uses that shape). Also drops their
#      orphaned .roles.client.<uuid> role-map entries so the baseline has no
#      dangling references to clients that no longer exist. Only UUID-KEYED map
#      entries are removed — the UUID *values* elsewhere (.id/.containerId/... on
#      flows, scopes, roles, orgs) are Keycloak's internal object ids, kept.
#   3. Replaces the KNOWN externally-configured credentials with ${IMPORT_KC_*}
#      placeholders that KC resolves from env vars at --import-realm:
#        pivox: the `start` client + the github / google / oidc.acme IdPs.
#        acme:  the `pivox` client (counterpart of pivox's oidc.acme IdP — they
#               share one credential).
#   4. DENY-BY-DEFAULT: removes every OTHER client secret, IdP clientSecret, and
#      smtp password so nothing real rides along. KC regenerates internal client
#      secrets (e.g. admin-permissions) on import.
#   5. Parameterizes the app URL: replaces the live host (https://$PIVOX_HOSTNAME)
#      with the ${IMPORT_KC_APP_URL} placeholder in every string value (audience
#      mappers, IdP broker endpoints, redirect URIs, ...) so the baseline isn't
#      pinned to one host; KC resolves it from env at --import-realm.
#   6. FAILS LOUD: after transforming, it scans the output and ABORTS if any
#      secret-ish value survives that is neither empty nor an ${IMPORT_KC_*}
#      placeholder — so a newly-added secret-bearing field (a new confidential
#      client, LDAP bindCredential, SAML signing key, etc.) can't silently leak;
#      you'll be told the exact path to handle.
#
#   7. USER exports (*-users-*.json): replaces every password credential with a
#      single ${IMPORT_KC_DEV_PASSWORD} placeholder, so the dev users can be
#      committed alongside the realms instead of being recreated by hand on every
#      fresh import.
#
#      A raw export writes passwords as argon2 hashes (secretData + credentialData
#      + a per-credential id and createdDate). Committing those would put password
#      hashes in the repo AND churn the diff on every re-export (fresh salt, fresh
#      id, fresh timestamp). KC accepts a PLAINTEXT `value` on import and hashes it
#      itself, so the whole credential array collapses to one deterministic entry —
#      no secret, no churn. One shared dev password across all accounts is the
#      deliberate trade (dev-only realms; the real value lives in .envrc).
#
#      Users with an EMPTY credentials array are left alone: those are
#      federated-only identities (social / SSO), and minting a local password for
#      them would change how they authenticate.
#
# The ${IMPORT_KC_*} names must match the apphost `withEnvironment` forwards and
# the .envrc exports. The real values live only in .envrc.
#
# Idempotent: known clients are matched by their real clientId (rewritten to a
# placeholder, so a re-run won't re-match); IdPs are matched by alias (not
# rewritten, but re-applying the same placeholder is a no-op); del() on an absent
# key is a no-op; the credential rewrite is a fixed value, so re-running is a no-op.
set -euo pipefail

# The live public host to strip out of the exports (replaced with the
# ${IMPORT_KC_APP_URL} placeholder). Same host the running KC served on —
# PIVOX_HOSTNAME (e.g. pivox.app) — so the committed baseline is never pinned
# to a specific dev's domain. Required.
: "${PIVOX_HOSTNAME:?set PIVOX_HOSTNAME to the public host to sanitize out}"
cd "$(dirname "$0")"

command -v jq >/dev/null 2>&1 || { echo "error: jq is required (brew install jq)" >&2; exit 1; }

tmp=""
cleanup() { [ -n "${tmp:-}" ] && rm -f "$tmp" || true; }
trap cleanup EXIT INT TERM

# Post-condition: emit the path of any string value that (a) lives under a key
# that actually holds secret material and (b) is neither empty nor an
# ${IMPORT_KC_*} placeholder. Non-empty output = a real secret survived.
#
# Matches EXACT path segments (key names), not substrings — so KC's policy/flow
# fields whose names merely contain "password"/"credential" (requiredCredentials,
# webAuthnPolicyPasswordless*, resetCredentialsFlow, authenticatorConfig,
# client.secret.creation.time, ...) are NOT false-flagged. Covers: client/idp
# secrets, smtp password, LDAP bindCredential, SAML/key-provider private keys.
#
# The second clause covers the USER files. A KC export writes password hashes as
# credentials[].secretData (caught by the first clause), but the sanitized form we
# write instead is a plaintext credentials[].value — a key name generic enough that
# the first clause would never flag it. Without this, pasting a real password into
# a user credential would sail straight through the scan into the repo.
LEAK_SCAN='
  [ paths(scalars) as $p
    | getpath($p)
    | select(type == "string" and . != "" and (startswith("${IMPORT_KC_") | not))
    | select(
        (($p | map(strings)) | any(.[];
            . == "secret" or . == "clientSecret" or . == "password"
            or . == "bindCredential" or . == "secretData"
            or test("private[._]?key$"; "i")))
        or
        ((($p | map(strings)) | any(.[]; . == "credentials")) and (($p | last) == "value"))
      )
    | ($p | map(tostring) | join(".")) ]
'

# Canonical ordering so re-exports diff cleanly instead of churning on
# Keycloak's random export order (both object-key order and array-element order
# vary run-to-run). Applied AFTER the transform so placeholders (e.g.
# ${IMPORT_KC_START_CLIENT_ID}) are already in place when sorting.
#
#   - Objects: keys sorted alphabetically. Sorting keys DURING the walk (not just
#     at output via -S) is load-bearing: it makes each element's `tojson` stable,
#     which the array tiebreaker below relies on.
#   - Arrays of objects: sorted by a stable, UNIQUE key. A non-unique key would
#     leave ties in Keycloak's random order and re-introduce churn — so we sort
#     by the natural identifier then break ties on the full canonical `tojson`.
#     EXCEPTION: authentication executions are ordered by their `priority` field
#     (Keycloak honors that, not array position), so those sort by priority.
#   - Scalar arrays (event types, algorithms, scope-name lists): all sets, sorted
#     directly.
#
# Purely reordering — no scalar value changes — so the realm imports identically
# and the leak scan (which walks paths, order-independent) still holds.
NORMALIZE='
  walk(
    if type == "object" then (to_entries | sort_by(.key) | from_entries)
    elif type == "array" and (length > 1) then
      if all(.[]; type == "object") then
        (if all(.[]; has("priority"))
           then sort_by([.priority, tojson])
           else sort_by([(.clientId // .name // .alias // .username // .containerId // .id // ""), tojson])
         end)
      elif all(.[]; (type | IN("string","number","boolean"))) then sort
      else . end
    else . end
  )
'

sanitize() { # sanitize <file> <transform-filter>
  local file="$1" filter="$2" leaks
  [ -f "$file" ] || { echo "error: $file not found — export the realm into this directory first" >&2; exit 1; }
  tmp="$(mktemp "${file}.XXXXXX")"
  jq "$filter | ($NORMALIZE)" "$file" >"$tmp" || { echo "error: jq transform failed on $file" >&2; exit 1; }
  leaks="$(jq -r "$LEAK_SCAN | .[]" "$tmp")" || { echo "error: jq scan failed on $file" >&2; exit 1; }
  if [ -n "$leaks" ]; then
    echo "error: $file still has non-placeholder secret(s) at:" >&2
    printf '  - %s\n' $leaks >&2
    echo "  -> handle them in the transform in $(basename "$0") (placeholder or remove)." >&2
    exit 1
  fi
  mv "$tmp" "$file"; tmp=""
  echo "sanitized $file"
}

# Deny-by-default deletes are guarded with `| startswith("${IMPORT_KC_") | not`
# so re-running doesn't strip the placeholders we just set (idempotency).

sanitize pivox-realm.json '
  del(.components["org.keycloak.keys.KeyProvider"])
  | (if has("clients") then .clients |= map(select((.clientId // "")
      | test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$") | not)) else . end)
  | (if (.roles.client | type) == "object" then .roles.client |= with_entries(select(.key
      | test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$") | not)) else . end)
  | (if has("clients") then .clients |= map(
      if .clientId == "start"
        then .clientId = "${IMPORT_KC_START_CLIENT_ID}" | .secret = "${IMPORT_KC_START_CLIENT_SECRET}"
      elif has("secret") and ((.secret // "") | startswith("${IMPORT_KC_") | not) then del(.secret)
      else . end) else . end)
  | (if has("identityProviders") then .identityProviders |= map(
      if   .alias == "github"    then .config.clientId = "${IMPORT_KC_IDP_GITHUB_CLIENT_ID}"    | .config.clientSecret = "${IMPORT_KC_IDP_GITHUB_CLIENT_SECRET}"
      elif .alias == "google"    then .config.clientId = "${IMPORT_KC_IDP_GOOGLE_CLIENT_ID}"    | .config.clientSecret = "${IMPORT_KC_IDP_GOOGLE_CLIENT_SECRET}"
      elif .alias == "oidc.acme" then .config.clientId = "${IMPORT_KC_IDP_OIDC_ACME_CLIENT_ID}" | .config.clientSecret = "${IMPORT_KC_IDP_OIDC_ACME_CLIENT_SECRET}"
      elif (.config | type == "object" and has("clientSecret")) and ((.config.clientSecret // "") | startswith("${IMPORT_KC_") | not) then del(.config.clientSecret)
      else . end) else . end)
  | (if (.smtpServer | type == "object" and has("password")) and ((.smtpServer.password // "") | startswith("${IMPORT_KC_") | not) then del(.smtpServer.password) else . end)
  | walk(if type == "string" then gsub("https://" + (env.PIVOX_HOSTNAME | gsub("[.]"; "\\.")); "${IMPORT_KC_APP_URL}") else . end)
'

sanitize acme-realm.json '
  del(.components["org.keycloak.keys.KeyProvider"])
  | (if has("clients") then .clients |= map(select((.clientId // "")
      | test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$") | not)) else . end)
  | (if (.roles.client | type) == "object" then .roles.client |= with_entries(select(.key
      | test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$") | not)) else . end)
  | (if has("clients") then .clients |= map(
      if .clientId == "pivox"
        then .clientId = "${IMPORT_KC_IDP_OIDC_ACME_CLIENT_ID}" | .secret = "${IMPORT_KC_IDP_OIDC_ACME_CLIENT_SECRET}"
      elif has("secret") and ((.secret // "") | startswith("${IMPORT_KC_") | not) then del(.secret)
      else . end) else . end)
  | (if has("identityProviders") then .identityProviders |= map(
      if (.config | type == "object" and has("clientSecret")) and ((.config.clientSecret // "") | startswith("${IMPORT_KC_") | not) then del(.config.clientSecret) else . end) else . end)
  | (if (.smtpServer | type == "object" and has("password")) and ((.smtpServer.password // "") | startswith("${IMPORT_KC_") | not) then del(.smtpServer.password) else . end)
  | walk(if type == "string" then gsub("https://" + (env.PIVOX_HOSTNAME | gsub("[.]"; "\\.")); "${IMPORT_KC_APP_URL}") else . end)
'

# --- user exports -------------------------------------------------------------
# Collapse every password credential to the shared ${IMPORT_KC_DEV_PASSWORD}
# placeholder. See item 7 in the header for why (no hashes in git, no diff churn).
# Federated-only users (empty credentials) are left untouched.
#
# Globbed rather than named: the set of *-users-N.json files depends on what the
# export produced (KC shards users across -0, -1, ... above a threshold), so
# hardcoding names would silently skip a shard — and a skipped shard is a shard
# full of password hashes going into the repo.
# Only PASSWORD credentials are placeholdered. Any other credential type (otp,
# webauthn, ...) is DROPPED rather than rewritten: replacing the whole array
# wholesale would silently turn a user's OTP secret into a password, and keeping
# it would commit real credential material. Dev realms have passwords only today —
# this is here so that stays true if someone enrolls 2FA and re-exports.
USERS_TRANSFORM='
  (if has("users") then .users |= map(
      .credentials = ((.credentials // [])
        | map(select(.type == "password"))
        | if length > 0
            then [{"type": "password", "value": "${IMPORT_KC_DEV_PASSWORD}"}]
            else [] end)) else . end)
  | walk(if type == "string" then gsub("https://" + (env.PIVOX_HOSTNAME | gsub("[.]"; "\\.")); "${IMPORT_KC_APP_URL}") else . end)
'

shopt -s nullglob
users_files=(*-users-*.json)
shopt -u nullglob
if [ ${#users_files[@]} -eq 0 ]; then
  echo "warning: no *-users-*.json found — dev users will not be part of the baseline" >&2
else
  for f in "${users_files[@]}"; do
    sanitize "$f" "$USERS_TRANSFORM"
  done
fi

echo "done — review the diff before committing."
