#!/usr/bin/env bash
# Refuse to commit or build content that contains a known-sensitive string.
#
# This exists because of P0-1c: the plan document explaining why the cluster's
# node name must never reach a public repository spelled that name out, and was
# pushed. Every safeguard in place at the time pointed at screenshots — a
# redaction flag, a replacement counter, a manual review of every image. None of
# them looked at prose.
#
# The needles cannot live in the repository they guard, so they come from
# somewhere untracked:
#
#   SENSITIVE_PATTERNS       newline-separated regexes (CI: a repository secret)
#   SENSITIVE_PATTERNS_FILE  path to a file of regexes (default .sensitive-patterns,
#                            which is gitignored)
#
# With neither set the check **skips**. That is deliberate: a fork or an outside
# PR has no access to the secret, and failing their builds over a check they
# cannot satisfy would only get the check deleted.
#
#   scripts/check-sensitive.sh            # everything tracked
#   scripts/check-sensitive.sh --staged   # what is about to be committed
set -euo pipefail

mode="${1:-}"

patterns_file="${SENSITIVE_PATTERNS_FILE:-.sensitive-patterns}"
cleanup=""
if [ -n "${SENSITIVE_PATTERNS:-}" ]; then
  cleanup=$(mktemp)
  printf '%s\n' "$SENSITIVE_PATTERNS" > "$cleanup"
  patterns_file="$cleanup"
fi
trap '[ -n "$cleanup" ] && rm -f "$cleanup"' EXIT

if [ ! -r "$patterns_file" ]; then
  echo "check-sensitive: no pattern source (SENSITIVE_PATTERNS or $patterns_file) — skipping."
  exit 0
fi

# Blank lines and comments would make git grep match every line, turning the
# check into a guaranteed failure that teaches people to bypass it.
usable=$(mktemp)
trap '[ -n "$cleanup" ] && rm -f "$cleanup"; rm -f "$usable"' EXIT
grep -v -e '^[[:space:]]*$' -e '^[[:space:]]*#' "$patterns_file" > "$usable" || true

if [ ! -s "$usable" ]; then
  echo "check-sensitive: pattern source is empty — skipping."
  exit 0
fi

scope=(--cached)
what="staged changes"
if [ "$mode" != "--staged" ]; then
  scope=()
  what="tracked files"
fi

# -I skips binaries: a PNG cannot be fixed by editing text, and screenshots are
# covered by verify-ui.mjs --redact instead (DESIGN §4.18).
if git grep -I -n -f "$usable" "${scope[@]}" -- . > /dev/null 2>&1; then
  echo "check-sensitive: found a sensitive string in $what:"
  echo
  # Show the location, never the value — printing it here would put it straight
  # into a CI log, which is the same mistake one layer down.
  git grep -I -l -f "$usable" "${scope[@]}" -- . | sed 's/^/  /'
  echo
  echo "Remove it before committing. If this is a false positive, fix the pattern,"
  echo "do not skip the check."
  exit 1
fi

echo "check-sensitive: clean ($what)."
