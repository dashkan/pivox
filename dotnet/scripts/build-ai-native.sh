#!/usr/bin/env bash
#
# Builds the two native AI-chat libraries (markdown C++ + highlight
# Rust) and stages the outputs into Pivox.Native/runtimes/<rid>/native/
# where the csproj packs them into the app bundle.
#
# Host-RID only — no cross-compilation. Run on each host that needs
# fresh binaries (developers per-platform, CI matrix per-RID).
#
# Single-config by default: optimized binaries with debug info
# preserved (C++ via -DCMAKE_BUILD_TYPE=RelWithDebInfo, Rust via
# debug=true in [profile.release]). See dotnet/CLAUDE.md for why
# we don't carry separate Debug + Release outputs.
#
# Usage:
#   dotnet/scripts/build-ai-native.sh             # release with symbols
#   dotnet/scripts/build-ai-native.sh --debug     # ad-hoc debug build
#                                                 # (overwrites release
#                                                 #  artifacts; rerun
#                                                 #  without --debug to
#                                                 #  restore)
#
# Requires:
#   - cmake, a C++20 compiler
#   - cargo + rustup
#   - vcpkg (for cmark-gfm); falls back to VCPKG_ROOT env var,
#     then $HOME/.vcpkg
#
# Idempotent: re-runs reuse cmake/cargo build caches.

set -euo pipefail

# ---- arg parsing -----------------------------------------------------

BUILD_TYPE="RelWithDebInfo"      # C++ build type
CARGO_PROFILE="release"          # Rust profile
CARGO_PROFILE_DIR="release"      # target/<dir> where Cargo lands artifacts

for arg in "$@"; do
    case "$arg" in
        --debug)
            BUILD_TYPE="Debug"
            CARGO_PROFILE="dev"
            CARGO_PROFILE_DIR="debug"
            ;;
        -h|--help)
            sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown arg: $arg" >&2
            exit 2
            ;;
    esac
done

# ---- paths -----------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DOTNET_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MARKDOWN_SRC="$DOTNET_DIR/native/markdown"
HIGHLIGHT_SRC="$DOTNET_DIR/native/highlight"
NATIVE_PROJ="$DOTNET_DIR/Pivox.Native"

# ---- RID detection ---------------------------------------------------

detect_rid() {
    local os arch
    case "$(uname -s)" in
        Darwin) os="osx" ;;
        Linux)  os="linux" ;;
        CYGWIN*|MINGW*|MSYS*) os="win" ;;
        *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
    esac
    case "$(uname -m)" in
        arm64|aarch64) arch="arm64" ;;
        x86_64|amd64)  arch="x64" ;;
        *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
    esac
    echo "$os-$arch"
}

RID="$(detect_rid)"
STAGE_DIR="$NATIVE_PROJ/runtimes/$RID/native"
mkdir -p "$STAGE_DIR"

echo "==> Host RID: $RID"
echo "==> Build:    $BUILD_TYPE / cargo $CARGO_PROFILE"
echo "==> Stage:    $STAGE_DIR"

# ---- vcpkg ------------------------------------------------------------

if [[ -z "${VCPKG_ROOT:-}" ]]; then
    VCPKG_ROOT="$HOME/.vcpkg"
fi
if [[ ! -f "$VCPKG_ROOT/scripts/buildsystems/vcpkg.cmake" ]]; then
    echo "vcpkg not found at $VCPKG_ROOT" >&2
    echo "Set VCPKG_ROOT or install vcpkg at \$HOME/.vcpkg" >&2
    exit 1
fi
TOOLCHAIN="$VCPKG_ROOT/scripts/buildsystems/vcpkg.cmake"

# ---- markdown (C++ via CMake) ----------------------------------------

echo "==> Building markdown ($BUILD_TYPE)"
MARKDOWN_BUILD="$MARKDOWN_SRC/build/$BUILD_TYPE"
cmake -S "$MARKDOWN_SRC" -B "$MARKDOWN_BUILD" \
    -DCMAKE_BUILD_TYPE="$BUILD_TYPE" \
    -DCMAKE_TOOLCHAIN_FILE="$TOOLCHAIN" >/dev/null
cmake --build "$MARKDOWN_BUILD" --config "$BUILD_TYPE" --parallel

# Copy artifacts. CMake names follow OS conventions; pick the matching
# extensions and the dSYM/pdb sidecars if produced.
case "$RID" in
    osx-*)
        cp "$MARKDOWN_BUILD/libpivox_markdown.dylib" "$STAGE_DIR/"
        if [[ -d "$MARKDOWN_BUILD/libpivox_markdown.dylib.dSYM" ]]; then
            cp -R "$MARKDOWN_BUILD/libpivox_markdown.dylib.dSYM" "$STAGE_DIR/"
        fi
        ;;
    win-*)
        cp "$MARKDOWN_BUILD/$BUILD_TYPE/pivox_markdown.dll" "$STAGE_DIR/"
        [[ -f "$MARKDOWN_BUILD/$BUILD_TYPE/pivox_markdown.pdb" ]] && \
            cp "$MARKDOWN_BUILD/$BUILD_TYPE/pivox_markdown.pdb" "$STAGE_DIR/"
        ;;
    linux-*)
        cp "$MARKDOWN_BUILD/libpivox_markdown.so" "$STAGE_DIR/"
        ;;
esac

# ---- highlight (Rust via Cargo) --------------------------------------

echo "==> Building highlight (cargo $CARGO_PROFILE)"
(
    cd "$HIGHLIGHT_SRC"
    if [[ "$CARGO_PROFILE" == "release" ]]; then
        cargo build --release
    else
        cargo build
    fi
)

HIGHLIGHT_TARGET="$HIGHLIGHT_SRC/target/$CARGO_PROFILE_DIR"
case "$RID" in
    osx-*)
        cp "$HIGHLIGHT_TARGET/libpivox_highlight.dylib" "$STAGE_DIR/"
        ;;
    win-*)
        cp "$HIGHLIGHT_TARGET/pivox_highlight.dll" "$STAGE_DIR/"
        [[ -f "$HIGHLIGHT_TARGET/pivox_highlight.pdb" ]] && \
            cp "$HIGHLIGHT_TARGET/pivox_highlight.pdb" "$STAGE_DIR/"
        ;;
    linux-*)
        cp "$HIGHLIGHT_TARGET/libpivox_highlight.so" "$STAGE_DIR/"
        ;;
esac

echo "==> Done. Artifacts in $STAGE_DIR:"
ls -la "$STAGE_DIR"
