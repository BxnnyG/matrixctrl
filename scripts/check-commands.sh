#!/usr/bin/env bash
# Every command in the docs must be runnable as written.
#
# The README contained `helm upgrade matrixctrl … --set secrets.adminPassword=…`,
# where the `…` stood for "the rest of your usual flags". An operator locked out
# of their install pasted it and got:
#
#   Error: non-absolute URLs should be in form of repo_name/path_to_chart,
#          got: %E2%80%A6
#
# Prose may abbreviate. A block someone is told to run may not: it is an
# instruction, and an instruction with a gap in it is a trap for exactly the
# reader who has no idea what belongs in the gap.
set -euo pipefail

status=0
for doc in README.md docs/*.md; do
  [ -f "$doc" ] || continue
  awk -v doc="$doc" '
    /^```(bash|sh|console|shell)/ { in_block = 1; next }
    /^```/                        { in_block = 0; next }
    in_block && /…/ { printf "%s:%d: %s\n", doc, NR, $0; found = 1 }
    END { exit found ? 1 : 0 }
  ' "$doc" || status=1
done

if [ "$status" != 0 ]; then
  echo "check-commands: the lines above are inside a shell block and contain '…'."
  echo "  Write the whole command, or move the abbreviation into the prose."
  exit 1
fi
echo "check-commands: every shell block is pasteable"
