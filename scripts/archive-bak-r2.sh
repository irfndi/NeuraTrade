#!/usr/bin/env bash
# P1: off-box copy of the same-host .bak to R2, then delete. Fail-closed.
# Usage: ./scripts/archive-bak-r2.sh [--delete-after-verify]
# Chunked gzip (1GB parts) so a 6.9GB single-PUT timeout can't strand the
# upload: each part PUTs + HEAD-verifies independently, and a re-run skips
# parts R2 already holds (resume). Deletion needs the explicit flag AND a
# manifest showing every part VERIFY OK (never auto-delete 6.9GB).
set -euo pipefail
ENV=/root/.neuratrade-r2.env
[ -f "$ENV" ] || { echo "r2 env missing ($ENV), abort"; exit 1; }
set -a
# shellcheck disable=SC1090
. "$ENV"
set +a
BAK=/root/.neuratrade/data/neuratrade.db.bak-20260905
[ -f "$BAK" ] || { echo "bak already gone, nothing to do"; exit 0; }
PUT=/opt/neuratrade/services/neuratrade-cli-ts/scripts/r2-put.py
# Stable workdir so resume is real (first run stamps PREFIX, re-runs reuse it).
WORK=/root/.neuratrade/archive-bak-20260905
mkdir -p "$WORK"
# Single-instance guard: MANIFEST read-modify-write and part-* cleanup race
# under concurrent runs, silently dropping verified state. flock is released
# automatically when the process exits (including on failure).
exec 9>"$WORK/LOCK"
flock -n 9 || { echo "another archive run is active ($WORK/LOCK held), abort" >&2; exit 1; }
# Atomic state write: a crash mid-write must never leave a half-written marker
# that the resume check below would accept (stale part counts / partial sha list
# or a truncated manifest line). Reads stdin, writes to $2.tmp, then renames.
write_atomic() {
  local out="$2" tmp="$2.tmp"
  cat >"$tmp"
  mv -f "$tmp" "$out"
}
# PREFIX binds resume state to this exact backup. The file stores
# "<backup-name> <timestamp>"; a PREFIX naming a different .bak (left over
# from editing BAK for a later backup) is rejected rather than silently
# reusing its timestamp in this backup's R2 key.
IDENT="$(basename "$BAK")"
if [ -f "$WORK/PREFIX" ]; then
  read -r p_ident p_ts _ <"$WORK/PREFIX" || true
  [ "${p_ident:-}" = "$IDENT" ] || {
    echo "stale PREFIX in $WORK names '${p_ident:-<empty>}', expected '$IDENT'; remove $WORK and re-run" >&2
    exit 1
  }
  TS=$p_ts
else
  TS=$(date -u +%Y%m%dT%H%M%SZ)
  write_atomic _ "$WORK/PREFIX" <<EOF
$IDENT $TS
EOF
fi
cleanup_parts() { rm -f "$WORK"/part-*; }
cleanup_split_state() {
  cleanup_parts
  rm -f "$WORK"/SPLIT_OK "$WORK"/SPLIT_OK.tmp "$WORK"/PARTS.sha256 "$WORK"/PARTS.sha256.tmp
  : >"$WORK/MANIFEST"
}
# Disk gate: the parts hold the gzip stream, whose size is unknowable in
# advance (SQLite .bak compresses ~3x, but incompressible data does not).
# Budget the full uncompressed size + 1GB headroom: an under-provisioned gate
# lets the split abort on ENOSPC mid-run, which is exactly the interrupted
# state this script must never leave.
AVAIL_KB=$(df -Pk "$WORK" | awk 'NR==2 {print $4}')
BAK_KB=$(du -k "$BAK" | cut -f1)
NEED_KB=$((BAK_KB + 1048576))
[ "$AVAIL_KB" -gt "$NEED_KB" ] || { echo "disk low (avail ${AVAIL_KB}KB < need ${NEED_KB}KB), abort"; exit 1; }
SRC_SHA=$(sha256sum "$BAK" | cut -d" " -f1)
echo "local sha256: $SRC_SHA ($(du -h "$BAK" | cut -f1))"
# 1GB gzip parts, numbered for resume.
# SPLIT_OK records the source sha256 + the expected part count, so "complete"
# means complete: an interrupted split (gzip killed, disk full, SIGTERM) leaves
# no marker, and the parts are rebuilt instead of being treated as a full set.
# SPLIT_OK also carries the backup identity, so a prior backup's markers can
# never validate this backup's parts.
SPLIT_OK="$WORK/SPLIT_OK"
PARTS_SHA="$WORK/PARTS.sha256"
split_valid=false
if [ -f "$SPLIT_OK" ] && [ -f "$PARTS_SHA" ] &&
  [ "$(cut -d' ' -f1 "$SPLIT_OK")" = "$SRC_SHA" ] &&
  [ "$(cut -d' ' -f3 "$SPLIT_OK")" = "$IDENT" ] &&
  (cd "$WORK" && sha256sum -c --quiet PARTS.sha256 >/dev/null 2>&1); then
  split_valid=true
fi
if [ "$split_valid" = false ]; then
  echo "splitting (no valid part set for sha $SRC_SHA): rebuilding parts from scratch"
  cleanup_split_state
  # A real pipeline (NOT process substitution): with `set -o pipefail` the
  # gzip exit status propagates, so a failing gzip aborts the split instead of
  # silently producing a truncated part set. Process substitution <(...) hides
  # gzip failures — that was the original data-loss bug. Target is the Linux
  # production host, where GNU split accepts "-" for stdin.
  if ! gzip -c "$BAK" | split -b 1G --numeric-suffixes=1 --suffix-length=3 - "$WORK/part-"; then
    cleanup_split_state
    echo "split FAILED; partial parts + manifest discarded, source untouched" >&2
    exit 1
  fi
  parts=$(find "$WORK" -maxdepth 1 -name 'part-*' -type f | wc -l | tr -d ' ')
  [ "$parts" -gt 0 ] || { echo "split produced no parts, abort" >&2; cleanup_split_state; exit 1; }
  # Per-part checksums: resume may skip the split only while every part on disk
  # still matches what the split produced (a truncated/edited part is rebuilt).
  # Order matters: PARTS.sha256 first, SPLIT_OK last — SPLIT_OK is the commit
  # marker, so its presence means the checksum list is already durable.
  (cd "$WORK" && sha256sum part-*) | write_atomic _ "$PARTS_SHA"
  write_atomic _ "$SPLIT_OK" <<EOF
$SRC_SHA $parts $IDENT
EOF
fi
EXPECTED=$(cut -d' ' -f2 "$SPLIT_OK" 2>/dev/null || true)
case "$EXPECTED" in
  '' | *[!0-9]*) echo "split marker malformed (EXPECTED='$EXPECTED'), abort" >&2; exit 1 ;;
esac
[ "$EXPECTED" -gt 0 ] || { echo "split marker malformed (EXPECTED=0), abort" >&2; exit 1; }
MANIFEST="$WORK/MANIFEST"
[ -f "$MANIFEST" ] || : > "$MANIFEST"
for part in "$WORK"/part-*; do
  n=$(basename "$part")
  # Key derived from the source file name, so state can never be shared
  # between two different .bak files through a stale prefix.
  key="neuratrade/db-backup/$(basename "$BAK")-$TS/$n.gz"
  # Exact-line, fixed-string, whole-line match: a part name can never be
  # mistaken for a longer name sharing a prefix, and regex metacharacters in
  # filenames cannot influence matching semantics.
  if grep -Fxq "$n VERIFY OK $key" "$MANIFEST" 2>/dev/null; then
    echo "skip $n (already verified)"
    continue
  fi
  python3 "$PUT" backtest-data "$key" "$part" --verify
  # Atomic append: a crash mid-append must not leave a partial line that a
  # later awk/grep count could read as a verified part.
  { cat "$MANIFEST"; echo "$n VERIFY OK $key"; } | write_atomic _ "$MANIFEST"
done
echo "all parts verified; manifest: $MANIFEST"
cat "$MANIFEST"
echo "NEXT: re-run with --delete-after-verify (checks manifest, asks YES, then rm + df)"
if [ "${1:-}" = "--delete-after-verify" ]; then
  actual=$(find "$WORK" -maxdepth 1 -name 'part-*' -type f | wc -l | tr -d ' ')
  # Distinct part names, not raw line count: a re-run that appends a duplicate
  # 'VERIFY OK' line must not inflate the count past EXPECTED and wedge the
  # delete gate either way.
  verified=$(awk '$2 == "VERIFY" { print $1 }' "$MANIFEST" | sort -u | wc -l | tr -d ' ')
  [ "$actual" = "$EXPECTED" ] || {
    echo "part set incomplete ($actual/$EXPECTED expected), abort"
    exit 1
  }
  [ "$verified" = "$EXPECTED" ] || { echo "manifest incomplete ($verified/$EXPECTED), abort"; exit 1; }
  # End-to-end proof before the only destructive step: the parts, concatenated
  # and decompressed, must reproduce the source byte-for-byte.
  echo "reassembling $EXPECTED parts and re-hashing against $SRC_SHA"
  # Explicit status handling: under `set -e`/`pipefail` a failing `gzip -dc`
  # would abort before the comparison, so capture and report it as a mismatch.
  GOT_SHA=$(cat "$WORK"/part-* | gzip -dc 2>/dev/null | sha256sum | cut -d" " -f1) || GOT_SHA=""
  [ -n "$GOT_SHA" ] || { echo "reassembly failed (gzip -dc errored); NOT deleting $BAK" >&2; exit 1; }
  [ "$GOT_SHA" = "$SRC_SHA" ] || {
    echo "reassembled sha256 $GOT_SHA != source $SRC_SHA; NOT deleting $BAK" >&2
    exit 1
  }
  echo "manifest complete ($verified/$EXPECTED) and reassembled sha256 matches. Type YES to delete $BAK"
  # Non-interactive stdin (EOF, cron, piped input) must abort, not proceed:
  # `read` returns non-zero on EOF, which `set -e` would honor anyway, but the
  # explicit message keeps the failure legible in logs.
  if ! read -r CONFIRM; then
    echo "no confirmation (stdin EOF); NOT deleting $BAK" >&2
    exit 1
  fi
  [ "$CONFIRM" = "YES" ] || { echo "aborted (confirmation != YES)"; exit 1; }
  # Re-hash the source immediately before the destructive step: the reassembly
  # proof above validates the parts against the pre-split hash, so it cannot
  # detect $BAK being replaced mid-run. Deleting a file whose current bytes are
  # not in R2 is unrecoverable.
  NOW_SHA=$(sha256sum "$BAK" | cut -d" " -f1)
  [ "$NOW_SHA" = "$SRC_SHA" ] || {
    echo "$BAK changed since split ($NOW_SHA != $SRC_SHA); NOT deleting" >&2
    exit 1
  }
  rm -v "$BAK"
  cleanup_split_state
  rm -f "$WORK/PREFIX"
  df -h / | tail -1
fi
