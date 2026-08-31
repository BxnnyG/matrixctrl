# Etappe 60 — The features nobody was taught

P2-14, verified against the code before acting on it — because the entry immediately
before it, P2-4, turned out to describe a feature that already exists, and I nearly
built it a second time.

P2-14 does hold. The README is 406 lines and covers install, DNS, ports, logs,
uninstall: everything about running the *container*. About the product it says:

| | mentions in README |
|---|---|
| what a hook is and why you want one | 0 (the word appears 6×, never explained) |
| the config editor | 0 |
| what to do when an upgrade fails | 0 |

Those three are not incidental features. **They are the reason this project exists** —
the whole premise is that manual patches survive `helm upgrade`, that ESS values can be
edited without hand-writing YAML and losing the comments that document it, and that an
upgrade is recoverable. All three ship, all three work, none is taught.

## What ships

`docs/GUIDE.md`, written for the person running a homeserver rather than for a
maintainer or an agent, linked from the README. Three chapters, one per gap.

The material is unusually good right now: this month produced a real 37-hour outage with
a complete diagnosis, and the panel gained a capacity preflight, an unschedulable
diagnosis and node history because of it. A guide written from that is concrete instead
of hypothetical — "what to do when an upgrade fails" can describe what actually
happened and what the screen shows.

## Rules it follows

**English**, like every other document here. The UI is German and stays that way until
Phase 6; the docs are the project's public face and have been English throughout.

**No invented screenshots and no invented UI.** Every claim is checked against the code
as it is written. Two documents in this repo have already described features that did
not exist (§4.17's audit trail, documented for two months before it was built) and one
described a fixed problem as open. A guide is a third opportunity to make the same
mistake, and the defence is the same: verify, then write.

**It teaches the trade-offs, not just the buttons.** Why hooks are ordered by priority,
why a config apply commits first, why deleting a room is deliberately absent. An
operator who knows only which button to press cannot tell a safe situation from an
unsafe one.

## Scope

**Does not ship: a reference for every screen.** Users, rooms, moderation and RTC are
discoverable — a list of rooms is a list of rooms. The three chapters cover what is
*not* discoverable, which is where the value is.

**Does not ship: screenshots.** The authenticated screens cannot be rendered here (the
same token wall as P2-32), and inventing them is worse than omitting them.

## Definition of done

- A reader who has never seen the panel can say what a hook is and why they want one
- The config editor's model is explained: sections, comments, diff, apply, rollback
- A failed upgrade has a documented path back, matching what the code actually does
- Every technical claim checked against the source while writing
- Linked from the README, English, `make check` green
