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
#   2. Replaces the KNOWN externally-configured credentials with ${IMPORT_KC_*}
#      placeholders that KC resolves from env vars at --import-realm:
#        pivox: the `start` client + the github / google / oidc.acme IdPs.
#        acme:  the `pivox` client (counterpart of pivox's oidc.acme IdP — they
#               share one credential).
#   3. DENY-BY-DEFAULT: removes every OTHER client secret, IdP clientSecret, and
#      smtp password so nothing real rides along. KC regenerates internal client
#      secrets (e.g. admin-permissions) on import.
#   4. FAILS LOUD: after transforming, it scans the output and ABORTS if any
#      secret-ish value survives that is neither empty nor an ${IMPORT_KC_*}
#      placeholder — so a newly-added secret-bearing field (a new confidential
#      client, LDAP bindCredential, SAML signing key, etc.) can't silently leak;
#      you'll be told the exact path to handle.
#
# The ${IMPORT_KC_*} names must match the apphost `withEnvironment` forwards and
# the .envrc exports. The real values live only in .envrc.
#
# Idempotent: known clients are matched by their real clientId (rewritten to a
# placeholder, so a re-run won't re-match); IdPs are matched by alias (not
# rewritten, but re-applying the same placeholder is a no-op); del() on an absent
# key is a no-op.
#
# NOTE: user exports (*-users-*.json) are intentionally NOT processed here — they
# carry password hashes and are gitignored (see .gitignore), not committed.
# Create dev users out of band (admin console / a seed), not via the baseline.
set -euo pipefail
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
LEAK_SCAN='
  [ paths(scalars) as $p
    | getpath($p)
    | select(type == "string" and . != "" and (startswith("${IMPORT_KC_") | not))
    | select( ($p | map(strings)) | any(.[];
          . == "secret" or . == "clientSecret" or . == "password"
          or . == "bindCredential" or . == "secretData"
          or test("private[._]?key$"; "i")) )
    | ($p | map(tostring) | join(".")) ]
'

sanitize() { # sanitize <file> <transform-filter>
  local file="$1" filter="$2" leaks
  [ -f "$file" ] || { echo "error: $file not found — export the realm into this directory first" >&2; exit 1; }
  tmp="$(mktemp "${file}.XXXXXX")"
  jq "$filter" "$file" >"$tmp" || { echo "error: jq transform failed on $file" >&2; exit 1; }
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
'

sanitize acme-realm.json '
  del(.components["org.keycloak.keys.KeyProvider"])
  | (if has("clients") then .clients |= map(
      if .clientId == "pivox"
        then .clientId = "${IMPORT_KC_IDP_OIDC_ACME_CLIENT_ID}" | .secret = "${IMPORT_KC_IDP_OIDC_ACME_CLIENT_SECRET}"
      elif has("secret") and ((.secret // "") | startswith("${IMPORT_KC_") | not) then del(.secret)
      else . end) else . end)
  | (if has("identityProviders") then .identityProviders |= map(
      if (.config | type == "object" and has("clientSecret")) and ((.config.clientSecret // "") | startswith("${IMPORT_KC_") | not) then del(.config.clientSecret) else . end) else . end)
  | (if (.smtpServer | type == "object" and has("password")) and ((.smtpServer.password // "") | startswith("${IMPORT_KC_") | not) then del(.smtpServer.password) else . end)
'

echo "done — review the diff before committing."
