#!/usr/bin/env bash
# Tests for release-meta.sh: which tag becomes which version, channel and image tags.
set -uo pipefail
cd "$(dirname "$0")"
fail=0
want() { # tag expected-multiline
  local got; got=$(./release-meta.sh "$1" 2>&1)
  if [ "$got" != "$2" ]; then echo "FAIL $1"; echo "$got" | sed 's/^/  got:  /'; fail=1; fi
}
want v0.1.128-rc.1 "VERSION=0.1.128-rc.1
BASE=0.1.128
CHANNEL=edge
PRERELEASE=true
IMAGE_TAGS=0.1.128-rc.1 edge"
want v0.1.128 "VERSION=0.1.128
BASE=0.1.128
CHANNEL=stable
PRERELEASE=false
IMAGE_TAGS=0.1.128 latest edge"
want 0.1.128 "$(./release-meta.sh v0.1.128)"
# an rc must never carry latest, a stable must
./release-meta.sh v1.2.3-rc.4 | grep -q 'IMAGE_TAGS=.*latest' && { echo "FAIL rc has latest"; fail=1; }
./release-meta.sh v1.2.3 | grep -q 'IMAGE_TAGS=.*latest' || { echo "FAIL stable lacks latest"; fail=1; }
for bad in "" v1.2 v1.2.3-beta v1.2.3-rc v1.2.3-rc.1.2 v1.2.3-rc.1x vx; do
  if ./release-meta.sh "$bad" >/dev/null 2>&1; then echo "FAIL accepted '$bad'"; fail=1; fi
done
[ $fail = 0 ] && echo "release-meta: ok"
exit $fail
