#!/usr/bin/env bash
# The chart version must have a CHANGELOG section.
#
# This check already existed — in the release workflow, which runs *after* a tag
# is pushed. v0.1.70 died there: the code was correct, tested and merged, the tag
# was public, and nothing was published. The operator's next `helm install`
# resolved the previous chart and hit the exact bug the release fixed, while every
# local signal said shipped.
#
# A guard that can only fire after the irreversible step is a report, not a guard.
# Same rule, moved to `make check`, where it costs a second and fires before the
# tag exists.
set -euo pipefail

chart="deploy/helm/matrixctrl/Chart.yaml"
version=$(awk '/^version:/{print $2; exit}' "$chart")
[ -n "$version" ] || { echo "check-changelog: no version in $chart"; exit 1; }

section=$(awk -v v="## [$version]" '
  index($0, v) == 1 { found = 1; next }
  found && /^## / { exit }
  found { print }
' CHANGELOG.md)

if [ -z "$(printf '%s' "$section" | tr -d '[:space:]')" ]; then
  echo "check-changelog: CHANGELOG.md has no '## [$version]' section."
  echo "  The release workflow fails on this too — but only once the tag is pushed."
  exit 1
fi

echo "check-changelog: CHANGELOG has $version ($(printf '%s\n' "$section" | grep -c . ) non-empty lines)"
