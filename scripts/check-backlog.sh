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
#
# What this cannot do is tell whether the declaration is TRUE. That limit was proved
# within the hour it was written: the same sweep marked P1-15 "Open" when the feature
# had been built — the check was satisfied, the reader was not. So a claim of "Open"
# has to carry the date it was last held against the code, and stale ones are named
# here. A status without a date is an opinion with no expiry.
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

# How long a check of an "Open" claim against the code stays believable.
STALE_DAYS = 90

# An entry ends where the next one begins. A fixed-size window does not: reading 400
# characters past a short entry reaches into its neighbour and finds that neighbour's
# status. Both checks below were written that way first, and the second one silently
# passed an entry that had no date because the next entry had one.
starts = [m.start() for m in re.finditer(r"^- (?:~~)?\*\*P[0-3]-", text, re.M)]
def entry_at(i):
    nxt = next((s for s in starts if s > i), len(text))
    return text[i:nxt]

bad = []
for m in re.finditer(r"^- (~~)?\*\*(P[0-3]-[0-9a-z]+) · (.{0,60})", text, re.M):
    if m.group(1):
        continue
    # The status has to be near the top, not buried — two lines, not the whole entry.
    head = "\n".join(entry_at(m.start()).split("\n")[:3])
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

# An "Open" claim has to say when it was last held against the code.
import datetime
today = datetime.date.today()
undated, stale = [], []
for m in re.finditer(r"^- \*\*(P[0-3]-[0-9a-z]+) · ", text, re.M):
    head = entry_at(m.start())
    if "**Open" not in head:
        continue
    d = re.search(r"verified (\d{4})-(\d{2})-(\d{2})", head)
    if not d:
        undated.append(m.group(1))
        continue
    age = (today - datetime.date(*map(int, d.groups()))).days
    if age > STALE_DAYS:
        stale.append((m.group(1), age))

if undated:
    print("check-backlog: these say Open without saying when that was last checked:")
    for tag in undated:
        print(f"  {tag}")
    print()
    print("  Write **Open (verified YYYY-MM-DD).** — a status with no date is an")
    print("  opinion with no expiry, and this file has produced three of those.")
    sys.exit(1)

total = len(re.findall(r"^- (?:~~)?\*\*P[0-3]-", text, re.M))
print(f"check-backlog: all {total} entries declare their status")
if stale:
    print(f"check-backlog: {len(stale)} open claim(s) not re-checked in over {STALE_DAYS} days:")
    for tag, age in stale:
        print(f"  {tag} — {age} days")
    print("  Not a failure. Hold them against the code and move the date, or close them.")
PY
