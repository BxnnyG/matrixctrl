# Etappe 49 — The check that passes when it checked nothing

`verify-ui.mjs` is the project's post-deploy UI check: walk every functional route in
a real browser, fail on console errors or an empty root, write a screenshot each. It
was found wanting on three separate counts while looking for something else, which is
becoming this project's most reliable way of finding defects.

## Finding 1 — it exits 0 having verified almost nothing

```js
process.exit(failed ? 1 : 0);
```

`skipped` is counted, printed, and **not in that expression**. Run without
`MATRIXCTRL_TOKEN`, ten of eleven routes skip and the process exits **0**. There is a
line of output saying so, but an exit status is what a Makefile, a CI job or a shell
`&&` reads, and this one says "pass".

This is §4.40 again, in the tool whose entire purpose is to answer "did it work?".
It is the same shape as `tsc --noEmit` against `"files": []`, which cost this project
a version it believed it had shipped — and the same shape as the quarantine `200 {}`
of E47. The defence written down after the last one was *read back the subject, not
the verdict*; here the subject is "how many routes were actually rendered".

**Fix:** a skipped route is a failure unless skipping was asked for. `--allow-skip`
makes the unauthenticated run legitimate and says so in the summary. Nothing about
the honest reporting changes — the tool always printed the truth. What changes is that
the exit code now agrees with it.

## Finding 2 — four screens are missing from the route list

`ROUTES` has eleven entries and stops at `/rtc`. The app has these too:

| screen | shipped | in ROUTES |
|---|---|---|
| `/users` | E27/E28 | ❌ |
| `/rooms` | E36/E41 | ❌ |
| `/rooms/{id}` | E41 | ❌ |
| `/reports` | E46, E47, E48 | ❌ |

Every one is Phase 2, every one shipped after the tool was written, and the report
queue has been changed by three consecutive etappes without once being opened in a
browser by this check. Meanwhile BACKLOG P1-5 still says it "drives chromium over all
nine functional routes", which was true when written and has been wrong for months.

`/rooms/{id}` needs a real room id, so it is **opt-in via `--room-id`** rather than a
permanent skip — a route that always skips would, under finding 1's fix, be a
permanent failure, and a check that always fails gets disabled within two etappes.

## Finding 3 — nothing runs it

No Makefile target, no CI step, no line in PROZESS §4's ship sequence. It is invoked
by remembering a `node scripts/…` incantation from a plan file written in E13. A
verification tool nobody runs is documentation.

**Fix:** `make verify-ui`, with `BASE` and `ROOM_ID` as variables.

## Scope

**Ships:** the exit-code fix, the four missing routes, the Makefile target, and the
stale coverage claims corrected.

**Does not ship: an authenticated run.** Reaching the ten protected routes needs a
session JWT. MatrixCtrl signs those with a key sitting in a cluster secret, and
minting one would be forging an admin session for myself — the same line drawn in
E47 over reading the operator's stored Matrix token (P2-32), and it should get the
same answer. The unauthenticated half is run and reported; the authenticated half is
one command for the operator, documented.

This is worth stating plainly: **this etappe fixes the check, it does not yet get the
check run over the screens it now covers.** Those are two different things and only
the first is delivered here.

## Definition of done

- No token → non-zero exit, with a message naming what was not verified
- `--allow-skip` → exit 0, and the summary says how many routes were skipped and why
- All four missing screens are in the route list
- `/rooms/{id}` runs only when `--room-id` is given
- `make verify-ui` works
- The "nine functional routes" claims are corrected wherever they appear
- Demonstrated by running both ways against the live 0.1.48
