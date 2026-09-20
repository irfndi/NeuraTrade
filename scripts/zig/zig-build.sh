#!/usr/bin/env bash
# P6: build the Rust workspace as versioned native binaries.
# Usage: ./scripts/zig/zig-build.sh [dev|release] [host|box]
# - host: native cargo build (local dev).
# - box: cross-build for x86_64-unknown-linux-gnu via cargo-zigbuild
#   (needs `cargo install cargo-zigbuild` + matching rust target once).
set -euo pipefail
PROFILE="${1:-release}"
DEST="${2:-host}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WANT_ZIG="$(cat "$ROOT/.zig-version")"
BEND_WANT="${BEND_VERSION:-2.0.21}"

have() { command -v "$1" >/dev/null 2>&1; }

if [ "$(bend version 2>/dev/null || echo none)" != "bend $BEND_WANT" ]; then
    echo "want bend $BEND_WANT ($(bend version 2>/dev/null || echo none) found)" >&2
    exit 1
fi
if [ "$DEST" = box ]; then
    TARGET="x86_64-unknown-linux-gnu"
    if ! have cargo-zigbuild; then
        echo "missing cargo-zigbuild (cargo install cargo-zigbuild)" >&2
        exit 1
    fi
    if [ "$(zig version 2>/dev/null || echo none)" != "$WANT_ZIG" ]; then
        echo "want zig $WANT_ZIG ($(zig version 2>/dev/null || echo none) found)" >&2
        exit 1
    fi
    (cd "$ROOT/crates" && cargo zigbuild --profile "$PROFILE" --target "$TARGET")
    ART="$ROOT/crates/target/$TARGET/$PROFILE/nt-cli"
    BEND_ART="$ROOT/crates/target/$TARGET/$PROFILE/nt-search"
else
    (cd "$ROOT/crates" && cargo build --profile "$PROFILE")
    ART="$ROOT/crates/target/$PROFILE/nt-cli"
    BEND_ART="$ROOT/crates/target/$PROFILE/nt-search"
fi
# fail-closed: bend search kernel compiles on the BUILD host arch (F32 must
# be compiled, never interpreted). CI zig job runs x86_64 so box nt-search
# is natively x86_64; a mac-arm64 host build yields arm64 (host-only).
bend "$ROOT/bend/search.bend" -o "$BEND_ART"
# fail-closed: refuse success without BOTH artifacts, then fingerprint them.
for f in "$ART" "$BEND_ART"; do
  if [ ! -x "$f" ]; then echo "missing artifact: $f" >&2; exit 1; fi
done
echo "artifacts: $ART $BEND_ART"
if have sha256sum; then sha256sum "$ART" "$BEND_ART"; else shasum -a 256 "$ART" "$BEND_ART"; fi
# health probes: nt-cli subcommand + nt-search ground-truth constant.
"$ART" health
test "$("$BEND_ART")" = "11"

