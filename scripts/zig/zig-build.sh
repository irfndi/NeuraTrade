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

have() { command -v "$1" >/dev/null 2>&1; }

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
    echo "artifacts: $ROOT/crates/target/$TARGET/$PROFILE/nt-cli"
else
    (cd "$ROOT/crates" && cargo build --profile "$PROFILE")
    echo "artifacts: $ROOT/crates/target/$PROFILE/nt-cli"
fi
