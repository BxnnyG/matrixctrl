# Etappe 47 — Quarantine returns 200 and sometimes does nothing

The half of Phase 2's moderation line that E46 deliberately left out. E46 built the
report queue; this is what an admin does about the media in one.

## Inventory, from Synapse's own source

Read out of the running container rather than from documentation — the source is the
authority, and the docs do not mention two of these endpoints at all:

| endpoint | method | notes |
|---|---|---|
| `/media/quarantine/<server>/<id>` | POST | quarantine one item |
| `/media/unquarantine/<server>/<id>` | POST | the inverse |
| `/media/<server>/<id>` | GET | **`media_info` with `quarantined_by`** |
| `/media/protect/<id>`, `/media/unprotect/<id>` | POST | mark safe from quarantine |
| `/room/<id>/media` | GET | media ids in a room |
| `/media/quarantine_changes` | GET | paginated log of quarantine changes |
| `/user_reports`, `/user_reports/<id>` | GET | a **second** report queue — see below |

## The finding this etappe is built around

`QuarantineMediaByID.on_POST` ends:

```python
await self.store.quarantine_media_by_id(server_name, media_id, requester.user.to_string())
return HTTPStatus.OK, {}
```

**It returns an empty body, always.** Not whether the media exists, not whether it
was already quarantined, not whether anything changed. And in the store:

```python
if quarantined_by is not None:
    hash_sql += " AND safe_from_quarantine = FALSE"
```

So quarantining media that has been *protected* is **silently skipped**, and the
caller receives `200 {}` — identical to success. The filter is applied only when
`quarantined_by is not None`, i.e. on quarantine and not on unquarantine, so the two
directions genuinely behave differently.

This is §4.20 and E41's rule with the volume turned up: a 200 says the request was
accepted, and here it does not even say that much about the outcome. **The state is
read back from `GET /media/<server>/<id>` after every write**, and the panel reports
what it found rather than what it asked for. When a quarantine request changes
nothing because the item is protected, the screen says exactly that — the operator
otherwise walks away believing they have taken something down.

## Media in a reported event

An event references media as `mxc://<server>/<id>` in `content.url`, and thumbnails
in `content.info.thumbnail_url`. Encrypted rooms put it under `content.file.url`
instead. All three are read; anything else is not guessed at.

An event with no media is the common case and says so plainly, rather than rendering
an empty panel that looks like a failure to load.

## Scope

**Ships:** media referenced by a reported event, its quarantine state read from
Synapse, and quarantine/unquarantine with read-back. Reversible in both directions,
which is what lets it ship now (§4.39).

**Does not ship: deleting media.** `DELETE /media/<server>/<id>` removes the file
permanently and has no inverse. Same treatment as room deletion and GDPR erasure —
its own etappe, or none.

**Does not ship: protect/unprotect.** It is a real pair and it would be easy, but
"protected" is a property an admin sets to stop *other* admins quarantining
something, and shipping the toggle in the same screen as the quarantine button
invites using it to win an argument. It belongs with a permissions story that does
not exist yet.

**Does not ship: `/user_reports`.** Discovered during this inventory: Synapse has a
**second** report queue, for reported *users* rather than events, with its own
endpoints. E46's screen does not know it exists. That is a gap in what shipped
yesterday and it is written down as one — but it is a queue, not a footnote, and
bolting it onto a media etappe would give it the same shallow treatment this plan
just refused for quarantine. Recorded in BACKLOG.

## Definition of done

- The media in a reported event is listed with its real quarantine state
- Quarantining then reading back shows the change, verified against live Synapse
- Quarantining protected media reports that nothing changed — the case the API
  cannot signal
- Unquarantine works on protected media, because Synapse's filter is one-directional
- An event with no media says so
- `make check` green

## What was actually verified, 2026-08-16 (0.1.47, revision 50)

Recorded because three of the six items above were **not** met on ship, and a plan
that quietly grades itself pass is worth less than no plan.

| item | status |
|---|---|
| media listed with its real state | ✅ code + unit tests; no live event with media to render yet |
| quarantine round trip against **live** Synapse | ❌ **not done** — see below |
| protected media reports "nothing changed" | ⚠️ source-verified + unit-tested, not live |
| unquarantine works on protected media | ⚠️ source-verified, not live |
| an event with no media says so | ✅ `Keine Dateien.`, unit-tested |
| `make check` green | ✅ CHECK=0, `check-sensitive: clean` |

Live, on the running 0.1.47: the route is registered and guarded (`PUT
/api/v1/media/{server}/{id}/quarantine` → 401), and the served chunk carries the
strings including the one this etappe exists for — *"Synapse hat die Anfrage
angenommen und nichts geändert — diese Datei ist vor Quarantäne geschützt."*

**Why the round trip is missing.** The admin API needs authority. Under MSC3861 this
deployment has no service-level admin credential: `users.admin = 1` is zero across all
four accounts, and admin authority rides on a MAS scope attached to the *operator's
own* token. Exercising the path would have meant lifting that stored credential out of
the database to act as them, writing to production media, and marking a real file
protected to reach the branch that matters. Reversible, but not mine to decide.
Tracked as **P2-31/P2-32** in BACKLOG; the operator can close it by clicking the
button once on a report that has an attachment.
