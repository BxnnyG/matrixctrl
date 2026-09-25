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

# A command that runs and answers a different question is the same betrayal of the
# reader as one that cannot be pasted — worse, because nothing about it looks wrong.
#
# `kubectl auth can-i create pods/exec` reads TYPE/NAME: it asks whether the subject
# may create a pod *called* exec. Subresources go in `--subresource=`. Both spellings
# run, neither errors, and they disagree. This one sat in doctor and in DESIGN §4.104
# as the evidence for a diagnosis, printing "no" six days after the right had been
# granted and sending the operator to run an upgrade that could not change it (§4.105).
#
# Scripts are checked as well as docs. A wrong command in a script is not a pasteable
# instruction, it is an executed one.
subresources='exec|log|token|scale|status|portforward|attach|finalize|proxy|approval|binding|eviction'
bad=$(
  # Docs: only inside a language-tagged block, the same line this file already draws.
  # An untagged ``` block in DESIGN or a plan is a transcript of what happened —
  # §4.104 quotes the broken command as evidence, and that quote must stay quotable.
  # A ```bash block is an instruction, and an instruction is where this bites.
  for doc in README.md docs/*.md; do
    [ -f "$doc" ] || continue
    awk -v doc="$doc" -v subs="$subresources" '
      /^```(bash|sh|console|shell)/ { in_block = 1; next }
      /^```/                        { in_block = 0; next }
      in_block && $0 ~ ("auth can-i") && $0 ~ ("[a-z]+/(" subs ")([^a-z]|$)") {
        printf "%s:%d: %s\n", doc, NR, $0
      }
    ' "$doc"
  done
  # Scripts: every line that is not a comment. A wrong command in a script is not a
  # pasteable instruction, it is an executed one.
  grep -rnE "auth can-i" scripts/*.sh \
    | grep -v 'check-commands.sh' \
    | grep -vE '^[^:]+:[0-9]+: *#' \
    | grep -E "[a-z]+/($subresources)([^a-z]|$)" || true
)
if [ -n "$bad" ]; then
  echo "$bad"
  echo "check-commands: 'auth can-i <verb> <type>/<subresource>' above asks about a"
  echo "  resource NAME, not a subresource, and answers a question nobody asked."
  echo "  Write: kubectl auth can-i <verb> <type> --subresource=<subresource>"
  exit 1
fi
echo "check-commands: no 'can-i' asks about a name where it means a subresource"
