#!/usr/bin/env bash
#
# Downloads the Firebase iOS SDK release zip and extracts the
# xcframeworks the FirebaseAuth binding needs into
# Firebase.Bindings/Frameworks/. Idempotent.
#
# We bind FirebaseAuth + FirebaseCore from C# and embed nine more
# transitive xcframeworks for the linker. See
# docs/dev/appkit-csharp.md in the pivox repo for the full binding
# workflow.
#
# Usage:
#   scripts/fetch-firebase-sdk.sh             # latest pinned version
#   FIREBASE_VERSION=12.13.0 scripts/...      # specific version

set -euo pipefail

FIREBASE_VERSION="${FIREBASE_VERSION:-12.13.0}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FRAMEWORKS_DIR="$ROOT/Firebase.Bindings/Frameworks"
TMP_DIR="$ROOT/.cache/firebase-sdk-$FIREBASE_VERSION"
ZIP="$TMP_DIR/Firebase.zip"
EXTRACTED="$TMP_DIR/Firebase"

# xcframeworks we need. Of these, FirebaseAuth + FirebaseCore are
# *bound* (C# can call their APIs); the rest are *embedded only*
# (linker resolves their symbols, no C# surface). See Mental Model
# section in the appkit-csharp doc.
AUTH_BUNDLE=(
    FirebaseAuth
    FirebaseAuthInterop
    FirebaseAppCheckInterop
    FirebaseCoreExtension
    GTMSessionFetcher
)
ANALYTICS_BUNDLE=(
    FirebaseCore
    FirebaseCoreInternal
    FirebaseInstallations
    GoogleUtilities
    nanopb
    FBLPromises
)

mkdir -p "$TMP_DIR" "$FRAMEWORKS_DIR"

if [[ ! -f "$ZIP" ]]; then
    echo "→ Downloading Firebase iOS SDK $FIREBASE_VERSION (~330 MB)…"
    curl -fL --progress-bar \
        -o "$ZIP" \
        "https://github.com/firebase/firebase-ios-sdk/releases/download/$FIREBASE_VERSION/Firebase.zip"
fi

if [[ ! -d "$EXTRACTED" ]]; then
    echo "→ Extracting…"
    (cd "$TMP_DIR" && unzip -q Firebase.zip)
fi

echo "→ Copying xcframeworks into $FRAMEWORKS_DIR/"
for fw in "${AUTH_BUNDLE[@]}"; do
    src="$EXTRACTED/FirebaseAuth/$fw.xcframework"
    dst="$FRAMEWORKS_DIR/$fw.xcframework"
    [[ -d "$src" ]] || { echo "  ✗ missing in SDK: $src" >&2; exit 1; }
    rm -rf "$dst"
    cp -R "$src" "$dst"
    echo "  ✓ $fw"
done
for fw in "${ANALYTICS_BUNDLE[@]}"; do
    src="$EXTRACTED/FirebaseAnalytics/$fw.xcframework"
    dst="$FRAMEWORKS_DIR/$fw.xcframework"
    [[ -d "$src" ]] || { echo "  ✗ missing in SDK: $src" >&2; exit 1; }
    rm -rf "$dst"
    cp -R "$src" "$dst"
    echo "  ✓ $fw"
done

echo
echo "Done. Total size:"
du -sh "$FRAMEWORKS_DIR"
echo
echo "Next: dotnet build Pivox.slnx"
