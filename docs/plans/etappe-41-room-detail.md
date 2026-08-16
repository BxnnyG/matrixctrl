# Etappe 41 — Rooms, second half: what is in a room, and the one action you can undo

**Status:** planned · 2026-08-16

## Where E36 stopped, and why

E36 shipped the room list and said plainly what it left out:

> Read-only: deleting, purging and blocking a room are each destructive in a
> different way and get their own etappe, as user writes did.

That was right about deleting and wrong to lump blocking in with it. Blocking is
**reversible** — it is a flag Synapse checks when someone tries to join, and
unblocking removes it. Deleting a room evicts every member and purges history, and
nothing brings it back.

So this etappe takes the two things an admin actually needs before deciding
anything, plus the one action that can be taken back:

- **Room detail** — who is in it, and what its state says
- **Block / unblock** — reversible, and the dialog says exactly what it does and,
  more importantly, what it does *not* do

Deleting stays out. It gets its own etappe, as user deactivation did in E28.

## Inventory, against the running Synapse

Probed on the live homeserver (401 means the route exists and wants a token, which
is the answer being looked for):

| endpoint | status |
|---|---|
| `GET /_synapse/admin/v1/rooms/<id>/members` | 401 |
| `GET /_synapse/admin/v1/rooms/<id>/state` | 401 |
| `PUT|GET /_synapse/admin/v1/rooms/<id>/block` | 401 |
| `GET /_synapse/admin/v2/rooms/<id>` | **405** — that route is DELETE-only |

The last row matters: the v2 path is the *delete* endpoint, and reaching for it to
read a room would be reaching for the one thing this etappe is not doing. Room
details come from the v1 route.

## What "blocked" actually means, because the dialog has to say it

Synapse's block flag prevents **new joins**. It does not:

- remove anyone already in the room
- delete any messages
- stop existing members from talking

An admin who blocks a room to stop an ongoing problem and walks away has not
stopped it. E28 established this pattern for users — "every dialog states what the
verb actually does" — after `deactivate` turned out to GDPR-erase by default. The
same rule applies here, and the failure mode is the mirror image: a verb that
sounds more final than it is.

Unblocking is offered in the same place, because a control you cannot find the
reverse of is one people avoid using at all.

## Design

`internal/synapse/rooms.go` already has the client, the token source, and the error
classification that separates "sign in again" (401 → 409 to the browser) from "not
an admin" (403). This adds:

- `RoomDetail`, `Member`, `MemberPage` — and members are paged, because a room can
  have thousands and the list page already learned that lesson
- `BlockState` / `SetBlocked` — read and write the flag, with the read used to
  render the current state rather than assuming it

The handler gains `GET /api/v1/rooms/{id}`, `GET /api/v1/rooms/{id}/members`, and
`PUT /api/v1/rooms/{id}/block`. The write goes through the audit middleware, which
already records mutations — a moderation action that nothing records is exactly
what E17 existed to fix.

**Room IDs contain `!` and `:`**, which is the detail most likely to break this
quietly. They are path-escaped on the way out and read with chi's URL param on the
way in; a test covers a real ID rather than a friendly placeholder.

## What could go wrong

- **The room does not exist**, or the ID is malformed. Synapse answers `M_NOT_FOUND`
  or `M_INVALID_PARAM`; both must read as "this room, not your session", so neither
  may be collapsed into the 409 that means "reconnect".
- **A blocked room still shows members.** That is correct and is exactly why the
  dialog says so.
- **Blocking the room the admin is in.** Permitted — Synapse allows it, and it does
  not eject them. Not guarded, but stated.

## Definition of done

- Room detail and paged members render for a real room on the live homeserver
- Block and unblock both work, and the state shown afterwards is *read back* rather
  than assumed from the request having succeeded
- The dialog states what blocking does not do
- The action appears in the audit log
- Signing in still works, S11 green after the deploy
