#!/usr/bin/env bash
# Every backlog entry must say what it is at the point where it is read.
#
# The entries are honest — the status is in there. It is just often four paragraphs
# down, under a heading that reads as broken work. That is enough to mislead: on
# 2026-09-08 a review of this file reported "15 open items" to the operator, when three
# of them had been built months earlier and several others said, in their own text,
# that they were deliberately waiting.
#
# It is not a new failure. E38 found eight entries describing work that already existed,
# and P2-4 was very nearly rebuilt for the same reason — its own note says the defence
# is to check an entry against the code before acting on it. This is that defence, made
# cheap: an entry that is not struck through must declare itself in its first line.
set -euo pipefail

FILE="${1:-docs/BACKLOG.md}"
[ -r "$FILE" ] || { echo "check-backlog: cannot read $FILE"; exit 1; }

python3 - "$FILE" <<'PY'
import re, sys

path = sys.argv[1]
text = open(path).read()

# Struck-through entries are unambiguous: done, and they read that way at a glance.
# Everything else has to carry one of these within its opening line.
TOKENS = (
    "**Open", "**Done", "**Partly", "**Mostly", "**Deliberately", "**Not built",
    "**Two of", "**First half", "⚠️", "⏳", "✅", "ANSWERED", "declined",
)

bad = []
for m in re.finditer(r"^- (~~)?\*\*(P[0-3]-[0-9a-z]+) · (.{0,60})", text, re.M):
    if m.group(1):
        continue
    head = text[m.start():m.start() + 300]
    if not any(t in head for t in TOKENS):
        bad.append((m.group(2), m.group(3).strip()))

if bad:
    one = len(bad) == 1
    print(f"check-backlog: {len(bad)} entr{'y' if one else 'ies'} {'does' if one else 'do'} not say what it is:")
    for tag, desc in bad:
        print(f"  {tag}: {desc[:64]}")
    print()
    print("  Put a status in the first line — **Open.**, **Done (E##) — kept for the")
    print("  lesson.**, **Open — blocked on P#-#.**, **Deliberately not now.** —")
    print("  so a reader scanning headings gets the same answer as one reading every word.")
    sys.exit(1)

total = len(re.findall(r"^- (?:~~)?\*\*P[0-3]-", text, re.M))
print(f"check-backlog: all {total} entries declare their status")
PY
