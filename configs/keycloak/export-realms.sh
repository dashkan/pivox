#!/usr/bin/env bash
#
# export-realms.sh — pull a fresh realm export out of the running Aspire Keycloak
# container and turn it into the committable baseline.
#
# Run with the Aspire stack up. It:
#   1. Runs `kc.sh export` INSIDE the keycloak container (reads the live DB,
#      writes realm + per-realm user JSON to a temp dir).
#   2. Copies every realm out EXCEPT master (pivox + acme) into this directory.
#   3. Runs sanitize-realms.sh to scrub secrets/key material so the *-realm.json
#      files are committable.
#
# The *-users-*.json files are copied too (for local re-import) but are gitignored
# — they carry password hashes and must never be committed.
#
# Notes:
#   - `kc.sh export` boots a second KC process against the same DB. It exports
#     fine, but then tries to bind the management interface (:9000), which the
#     running server already holds — so we hand it a free management port and
#     gate success on the produced files, not kc.sh's exit code.
set -euo pipefail
cd "$(dirname "$0")"

KC="$(docker ps --format '{{.Names}}' | grep -i keycloak | head -1)"
[ -n "$KC" ] || {
  echo "error: no running keycloak container found — is the Aspire stack up?" >&2
  exit 1
}
echo "keycloak container: $KC"

EXPORT_DIR=/tmp/kc-export
docker exec "$KC" rm -rf "$EXPORT_DIR" 2>/dev/null || true

echo "exporting realms inside the container…"
# Free management port avoids the :9000 clash with the running server; `|| true`
# because kc.sh may still exit non-zero on the post-export bootstrap — the file
# check below is the real success gate.
docker exec -e KC_HTTP_MANAGEMENT_PORT=9100 "$KC" \
  /opt/keycloak/bin/kc.sh export --dir "$EXPORT_DIR" --users different_files \
  >/dev/null 2>&1 || true

docker exec "$KC" sh -c "ls $EXPORT_DIR/pivox-realm.json" >/dev/null 2>&1 || {
  echo "error: export produced no pivox-realm.json — re-run kc.sh export without redirection to see the failure" >&2
  exit 1
}

echo "copying non-master realm + user files…"
copied=0
for f in $(docker exec "$KC" sh -c "ls -1 $EXPORT_DIR"); do
  case "$f" in
    master-*) echo "  skip   $f" ;;
    *.json)
      docker cp "$KC:$EXPORT_DIR/$f" "./$f" >/dev/null
      echo "  copied $f"
      copied=$((copied + 1))
      ;;
    *) echo "  ignore $f" ;;
  esac
done
docker exec "$KC" rm -rf "$EXPORT_DIR" 2>/dev/null || true
[ "$copied" -gt 0 ] || {
  echo "error: nothing copied" >&2
  exit 1
}

# sanitize-realms.sh strips dynamically-registered (DCR) clients from the realm
# baseline. But MCP clients require consent, so any user who authed to one holds
# a dangling clientConsents entry — on a fresh --import-realm KC aborts with
# "Unable to find client consent mappings for client: <uuid>". Strip those
# consents from the (gitignored, local-reimport) user files so they stay
# importable against the sanitized realm. Matches DCR clients by UUID-shaped
# clientId, same rule as sanitize-realms.sh.
echo "stripping DCR client consents from user files…"
uuid_re='^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
for uf in ./*-users-*.json; do
  [ -f "$uf" ] || continue
  utmp="$(mktemp "${uf}.XXXXXX")"
  jq --arg re "$uuid_re" '
    .users |= map(
      if has("clientConsents")
      then (.clientConsents |= map(select((.clientId // "") | test($re) | not)))
           | (if (.clientConsents | length) == 0 then del(.clientConsents) else . end)
      else . end)
  ' "$uf" >"$utmp" && mv "$utmp" "$uf" && echo "  cleaned $uf"
done

echo "sanitizing realm files…"
./sanitize-realms.sh

echo "done. *-realm.json are committable; *-users-*.json are gitignored (local re-import only)."
