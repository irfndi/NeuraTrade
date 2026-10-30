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
# Fail closed on bad inputs instead of passing them through to cargo/zig.
case "$PROFILE" in dev | release) ;; *)
  echo "unknown profile '$PROFILE' (want dev|release)" >&2
  exit 1
  ;;
esac
case "$DEST" in host | box) ;; *)
  echo "unknown dest '$DEST' (want host|box)" >&2
  exit 1
  ;;
esac
[ -f "$ROOT/.zig-version" ] || {
  echo "missing .zig-version" >&2
  exit 1
}
WANT_ZIG="$(cat "$ROOT/.zig-version")"
[ -n "$WANT_ZIG" ] || {
  echo "empty .zig-version" >&2
  exit 1
}
BEND_WANT="${BEND_VERSION:-2.0.21}"

have() { command -v "$1" >/dev/null 2>&1; }

# Capture separately from the fallback: `$(cmd || echo none)` would concatenate
# stdout with the fallback when the command prints and then fails.
have bend || {
  echo "bend not found (want $BEND_WANT)" >&2
  exit 1
}
GOT_BEND=$(bend version 2>/dev/null) || GOT_BEND=none
if [ "$GOT_BEND" != "bend $BEND_WANT" ]; then
  echo "want bend $BEND_WANT ($GOT_BEND found)" >&2
  exit 1
fi
if [ "$DEST" = box ]; then
  TARGET="x86_64-unknown-linux-gnu"
  if ! have cargo-zigbuild; then
    echo "missing cargo-zigbuild (cargo install cargo-zigbuild)" >&2
    exit 1
  fi
  have zig || {
    echo "zig not found (want $WANT_ZIG)" >&2
    exit 1
  }
  GOT_ZIG=$(zig version 2>/dev/null) || GOT_ZIG=none
  if [ "$GOT_ZIG" != "$WANT_ZIG" ]; then
    echo "want zig $WANT_ZIG ($GOT_ZIG found)" >&2
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
  if [ ! -x "$f" ]; then
    echo "missing artifact: $f" >&2
    exit 1
  fi
done
echo "artifacts: $ART $BEND_ART"
# box mode is a cross-build for x86_64-linux, but bend compiles nt-search for
# the BUILD host arch. Ship it only when the binary really is x86-64: a
# host-arch kernel placed under target/x86_64-.../ would still answer the local
# health probe and then fail on the box.
if [ "$DEST" = box ]; then
  have file || {
    echo "file(1) required to verify the box arch of $BEND_ART" >&2
    exit 1
  }
  if ! file -b "$BEND_ART" | grep -q 'x86-64'; then
    echo "nt-search is not x86-64 (bend builds for the host arch): $(file -b "$BEND_ART")" >&2
    exit 1
  fi
fi
if have sha256sum; then sha256sum "$ART" "$BEND_ART"; else shasum -a 256 "$ART" "$BEND_ART"; fi
# health probes: nt-cli subcommand + nt-search ground-truth constant.
"$ART" health
test "$("$BEND_ART")" = "11"
