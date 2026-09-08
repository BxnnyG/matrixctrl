#!/usr/bin/env bash
# Ask the registry what actually arrived.
#
# The release workflow pushes an image and a chart and is then finished. Whether the
# registry ended up holding what it believed it pushed was never checked — and on
# 2026-09-05 it held nothing at all, because the job had died at a guard before the push
# and nobody looked. The operator found out by installing the previous version (§4.75).
#
# The same shape, one level in: the image is built for two architectures, and if one of
# them silently stopped being produced, nothing would notice until somebody on an ARM
# board ran `helm install` and got "no matching manifest".
#
# The tag is the intention. The registry is the result.
#
#   scripts/check-published.sh 0.1.81
set -euo pipefail

VERSION="${1:-}"
[ -n "$VERSION" ] || { echo "usage: $0 <version>   e.g. $0 0.1.81"; exit 2; }

IMAGE_REPO="${IMAGE_REPO:-bxnnyg/matrixctrl}"
CHART_REPO="${CHART_REPO:-bxnnyg/charts/matrixctrl}"
REGISTRY="${REGISTRY:-https://ghcr.io}"
WANT_ARCHES="${WANT_ARCHES:-amd64 arm64}"

fail=0

token() {
  curl -fsSL "$REGISTRY/token?scope=repository:$1:pull&service=ghcr.io" \
    | sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
}

echo "Registry: $REGISTRY"
echo "Version:  $VERSION"
echo

# ── the chart ────────────────────────────────────────────────────────────────
echo "Chart $CHART_REPO"
if curl -fsSL -H "Authorization: Bearer $(token "$CHART_REPO")" \
     "$REGISTRY/v2/$CHART_REPO/tags/list" \
   | grep -q "\"$VERSION\""; then
  echo "  ✓ tag $VERSION is published"
else
  echo "  ✗ tag $VERSION is NOT in the tag list — helm install will resolve an older chart"
  fail=1
fi
echo

# ── the image, and every architecture in it ──────────────────────────────────
echo "Image $IMAGE_REPO"
IMAGE_TOKEN=$(token "$IMAGE_REPO")
manifest=$(curl -fsSL -H "Authorization: Bearer $IMAGE_TOKEN" \
  -H "Accept: application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.manifest.v1+json" \
  "$REGISTRY/v2/$IMAGE_REPO/manifests/$VERSION" 2>/dev/null || true)

if [ -z "$manifest" ]; then
  echo "  ✗ no manifest for $VERSION — the image was not published"
  exit 1
fi

found=$(printf '%s' "$manifest" | python3 -c '
import json,sys
try: d = json.load(sys.stdin)
except Exception: sys.exit(0)
ms = d.get("manifests")
if not ms:
    # A single manifest, not an index: one architecture only.
    print("single")
    sys.exit(0)
for m in ms:
    p = m.get("platform", {})
    arch, os_ = p.get("architecture", "?"), p.get("os", "?")
    # buildx attaches attestation manifests as unknown/unknown; they are not platforms.
    if arch == "unknown":
        continue
    print(os_ + "/" + arch, m.get("size", 0))
')

if [ "$found" = "single" ]; then
  echo "  ✗ the image is a single manifest, not a multi-architecture index"
  fail=1
else
  printf '%s\n' "$found" | while read -r platform size; do
    [ -n "$platform" ] && echo "  · $platform (manifest $size bytes)"
  done
  for arch in $WANT_ARCHES; do
    if printf '%s' "$found" | grep -q "linux/$arch"; then
      echo "  ✓ linux/$arch"
    else
      echo "  ✗ linux/$arch is missing — anyone on that architecture cannot pull this"
      fail=1
    fi
  done
fi

echo
if [ "$fail" != 0 ]; then
  echo "✗ what was pushed and what the registry holds do not agree."
  exit 1
fi
echo "✓ the registry holds what this release claims to have published."
