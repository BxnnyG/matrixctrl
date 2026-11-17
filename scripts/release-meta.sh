#!/usr/bin/env bash
# Tag -> release facts, in one place (etappe 122). release.yml reads these lines and
# derives nothing itself, so the rule "an rc never becomes latest" lives only here.
#
#   scripts/release-meta.sh v0.1.128-rc.1
#     VERSION=0.1.128-rc.1  BASE=0.1.128  CHANNEL=edge  PRERELEASE=true
#     IMAGE_TAGS=0.1.128-rc.1 edge
#   scripts/release-meta.sh v0.1.128
#     VERSION=0.1.128  BASE=0.1.128  CHANNEL=stable  PRERELEASE=false
#     IMAGE_TAGS=0.1.128 latest edge
set -euo pipefail

TAG="${1:-}"
VERSION="${TAG#v}"
if [[ ! "$VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+)(-rc\.[0-9]+)?$ ]]; then
  echo "release-meta: '$TAG' is neither vX.Y.Z nor vX.Y.Z-rc.N" >&2
  exit 2
fi
BASE="${BASH_REMATCH[1]}"

if [ -n "${BASH_REMATCH[2]}" ]; then
  CHANNEL=edge; PRERELEASE=true;  IMAGE_TAGS="$VERSION edge"
else
  CHANNEL=stable; PRERELEASE=false; IMAGE_TAGS="$VERSION latest edge"
fi

echo "VERSION=$VERSION"
echo "BASE=$BASE"
echo "CHANNEL=$CHANNEL"
echo "PRERELEASE=$PRERELEASE"
echo "IMAGE_TAGS=$IMAGE_TAGS"
