#!/usr/bin/env bash
#
# clean-fn-revisions.sh
#
# Cleans up Firebase Functions v2 deployments in Google Cloud (Cloud
# Run under the hood). Two phases:
#
#   1. ORPHAN SERVICES — Cloud Run services tagged as Firebase-managed
#      that no longer exist in `firebase functions:list`. Most often
#      arises after a function is renamed in source: the new export
#      gets a new Cloud Run service, and the old one is left behind
#      until something cleans it up. Phase 1 deletes those services
#      entirely.
#
#   2. STALE REVISIONS — for surviving services, every Cloud Run
#      revision that is NOT the actively-serving one. Cloud Run keeps
#      old revisions around indefinitely; this prunes anything not
#      currently routed to. The active revision is identified via
#      `status.traffic[].revisionName` so we never delete what's
#      serving traffic.
#
# Project + region are pulled from the active gcloud config so this
# works wherever you happen to be authed.
#
# Dry-run by default. Set FORCE=1 to actually issue deletes.

set -euo pipefail

DRY_RUN=true
if [[ "${FORCE:-}" == "1" ]]; then
  DRY_RUN=false
fi

PROJECT=$(gcloud config get-value project 2>/dev/null || true)
REGION=$(gcloud config get-value run/region 2>/dev/null || true)

if [[ -z "${PROJECT}" ]]; then
  echo "error: no active gcloud project (run: gcloud config set project <id>)" >&2
  exit 1
fi
if [[ -z "${REGION}" ]]; then
  REGION="us-central1"
  echo "note: no run/region set in gcloud config; defaulting to ${REGION}" >&2
fi

for cmd in gcloud firebase jq; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "error: required command '${cmd}' not found on PATH" >&2
    exit 1
  fi
done

echo "Project: ${PROJECT}"
echo "Region:  ${REGION}"
if ${DRY_RUN}; then
  echo "Mode:    DRY RUN (set FORCE=1 to delete)"
else
  echo "Mode:    DELETE"
fi
echo

# --- Phase 0: enumerate ---

# Cloud Run services managed by Cloud Functions / Firebase Functions
# v2. The label `goog-managed-by=cloudfunctions` is set automatically
# by the gcloud functions deploy / firebase deploy pipeline.
fb_services=$(
  gcloud run services list \
    --region="${REGION}" \
    --project="${PROJECT}" \
    --filter='metadata.labels.goog-managed-by=cloudfunctions' \
    --format='value(metadata.name)' \
    | sort -u
)

# Currently-deployed Firebase Functions per the Firebase CLI.
# `firebase functions:list --json` returns rows with `id` (the export
# name) and `platform`. Cloud Run service names are the lowercase
# form of the export name.
fb_deployed=$(
  firebase functions:list --json 2>/dev/null \
    | jq -r '.result[]? | select(.platform == "gcfv2") | .id' \
    | tr '[:upper:]' '[:lower:]' \
    | sort -u
)

echo "Firebase-managed Cloud Run services in ${REGION}:"
if [[ -z "${fb_services}" ]]; then
  echo "  (none)"
else
  echo "${fb_services}" | sed 's/^/  /'
fi
echo
echo "Firebase Functions currently deployed:"
if [[ -z "${fb_deployed}" ]]; then
  echo "  (none)"
else
  echo "${fb_deployed}" | sed 's/^/  /'
fi
echo

# --- Phase 1: orphan services ---

echo "=== Phase 1: orphaned services ==="
orphans=$(comm -23 <(echo "${fb_services}") <(echo "${fb_deployed}"))

if [[ -z "${orphans}" ]]; then
  echo "  (none)"
else
  while IFS= read -r svc; do
    [[ -z "${svc}" ]] && continue
    echo "  orphan: ${svc}"
    if ! ${DRY_RUN}; then
      gcloud run services delete "${svc}" \
        --region="${REGION}" --project="${PROJECT}" --quiet
    fi
  done <<<"${orphans}"
fi
echo

# --- Phase 2: stale revisions ---

echo "=== Phase 2: stale revisions ==="
surviving=$(comm -12 <(echo "${fb_services}") <(echo "${fb_deployed}"))

if [[ -z "${surviving}" ]]; then
  echo "  (no surviving services)"
else
  while IFS= read -r svc; do
    [[ -z "${svc}" ]] && continue

    # `status.traffic` is a list; in normal Firebase Functions deploys
    # it has exactly one entry routing 100% to the latest revision.
    # Concatenate just in case multiple traffic splits exist.
    active=$(
      gcloud run services describe "${svc}" \
        --region="${REGION}" --project="${PROJECT}" \
        --format='value(status.traffic[].revisionName)'
    )

    all_revs=$(
      gcloud run revisions list \
        --service="${svc}" \
        --region="${REGION}" --project="${PROJECT}" \
        --format='value(metadata.name)'
    )

    stale=$(
      comm -23 \
        <(echo "${all_revs}" | sort -u) \
        <(printf '%s\n' ${active} | sort -u)
    )

    if [[ -z "${stale}" ]]; then
      echo "  ${svc}: only active revision(s) — nothing to clean"
      continue
    fi

    echo "  ${svc}: active=${active//$'\n'/, }"
    while IFS= read -r rev; do
      [[ -z "${rev}" ]] && continue
      echo "    delete revision: ${rev}"
      if ! ${DRY_RUN}; then
        gcloud run revisions delete "${rev}" \
          --region="${REGION}" --project="${PROJECT}" --quiet
      fi
    done <<<"${stale}"
  done <<<"${surviving}"
fi

echo
if ${DRY_RUN}; then
  echo "Dry run complete. Run with FORCE=1 to actually delete."
fi
