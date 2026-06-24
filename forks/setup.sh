#!/usr/bin/env bash
#
# HyPanel fork bootstrap — run ONCE on the Linux build host before `./build.sh`.
#
# Why: Ф1 (restart-free Hysteria2 user add/ban + instant kick) needs two tiny
# patches to embedded sing-box / sing-quic that upstream does not expose. Rather
# than vendor those large trees, we keep only the two patched files
# (forks/patched-files/) in git, clone the pinned upstream here, overlay the
# patched files, and point go.mod's replace directives at the local clones.
#
# This script makes go.mod dirty BY DESIGN (it rewrites the sing-box/sing-quic
# replace lines to local paths). To return to upstream resolution:  git checkout go.mod
#
# Idempotent: safe to re-run.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

# Must match the pins in the committed go.mod:
#   replace github.com/sagernet/sing-box => ... -78b2e12fbdd8   (require/indirect sing-quic v0.6.1)
SB_COMMIT="78b2e12fbdd8"
SQ_REF="v0.6.1"

SB_DIR="$HERE/sing-box"
SQ_DIR="$HERE/sing-quic"

clone_at() { # <git-url> <dir> <ref>
  local url="$1" dir="$2" ref="$3"
  if [ ! -d "$dir/.git" ]; then
    echo "==> cloning $url -> ${dir#$ROOT/}"
    git clone --quiet "$url" "$dir"
  fi
  echo "==> checking out $ref in ${dir#$ROOT/}"
  if ! git -C "$dir" checkout --quiet "$ref" 2>/dev/null; then
    # commit not on a local branch tip — fetch it explicitly (GitHub allows reachable-SHA fetch)
    git -C "$dir" fetch --quiet --tags origin "$ref"
    git -C "$dir" checkout --quiet FETCH_HEAD
  fi
}

clone_at https://github.com/sagernet/sing-box  "$SB_DIR" "$SB_COMMIT"
clone_at https://github.com/sagernet/sing-quic "$SQ_DIR" "$SQ_REF"

echo "==> overlaying patched files"
cp "$HERE/patched-files/sing-box/protocol/hysteria2/inbound.go" "$SB_DIR/protocol/hysteria2/inbound.go"
cp "$HERE/patched-files/sing-quic/hysteria2/service.go"         "$SQ_DIR/hysteria2/service.go"

echo "==> pointing go.mod replace directives at the local forks"
( cd "$ROOT" \
  && go mod edit -replace "github.com/sagernet/sing-box=./forks/sing-box" \
  && go mod edit -replace "github.com/sagernet/sing-quic=./forks/sing-quic" )

cat <<EOF

Done. go.mod now resolves sing-box/sing-quic from ./forks/* (patched).
Next:
  cd "$ROOT"
  go mod tidy          # reconcile go.sum for the local replaces
  ./build.sh           # CGO build (needs gcc/musl-dev + the s-ui build tags)

To revert to upstream resolution:  git -C "$ROOT" checkout go.mod
EOF
