# Etappe 46 — The report queue, and what "handled" means

Phase 2's last piece. Users, rooms and room actions ship; "Reports & moderation —
event report queue, media quarantine" is what stands between this project and its
stated deliverable, *existing ESS admins can drop element-admin*.

## Inventory

Probed against the live Synapse **v1.158.0**, by method, because a path that exists
and a method that is allowed are different questions (E41 learned this from
`/_synapse/admin/v2/rooms/<id>`, which is DELETE-only and answers 405 to GET):

| endpoint | GET | POST | DELETE |
|---|---|---|---|
| `/_synapse/admin/v1/event_reports` | 401 | — | — |
| `/_synapse/admin/v1/event_reports/<id>` | 401 | — | 401 |
| `/_synapse/admin/v1/media/quarantine/<server>/<id>` | 405 | 401 | — |
| `/_synapse/admin/v1/media/unquarantine/<server>/<id>` | — | 401 | — |
| `/_synapse/admin/v1/room/<id>/media` | 401 | — | — |
| `/_synapse/admin/v1/room/<id>/media/quarantine` | 405 | 401 | — |

401 means the endpoint is there and wants a token; every one of them exists on this
server.

**The token machinery already exists and is reused, not rebuilt.** E36/E42 established
the operator-authority flow: `h.client(userID)`, `writeSynapseError`, the deliberate
409-not-401 for an expired Matrix token, and `ConnectPanel` on the frontend. Reports
need exactly the same gate, so they get exactly the same one.

**The sidebar already has a `moderation` entry** with a shield icon and no `to:` —
a placeholder from the original navigation. It gets a route rather than a new item.

## The design question this etappe is actually about

A queue needs a way to empty it. Synapse offers one: `DELETE /event_reports/<id>`,
which removes the record permanently. There is no "resolved" state in Synapse — a
report is either present or destroyed.

That is the wrong primitive to build a moderation queue on, for two reasons:

- **It destroys the evidence.** A report is a user's statement that something was
  wrong. Deleting it after acting on it means the next admin cannot see that the
  report existed, what it said, or that anyone looked at it. If the same account is
  reported five times and each report is deleted as it is handled, the pattern — the
  thing that actually matters — is erased one report at a time.
- **It is irreversible**, and §4.39 is the standing rule here: an action with an
  inverse ships early, one without gets its own careful treatment.

So **disposition is recorded in MatrixCtrl, not in Synapse.** A report is marked
handled or dismissed, by whom, when, with an optional note; Synapse's record is left
untouched. The queue defaults to the open ones and the handled ones stay reachable.
Marking is reversible — reopening a report takes nothing away.

This also happens to be the first row of Phase 5's unified audit story, arrived at
because it was the right answer here rather than because it was on a list.

## The field pair that will be got backwards

Synapse's report object carries both:

- `user_id` — the person who **filed** the report
- `sender` — the person who **sent the reported event**

Two user IDs on one record, where mixing them up accuses the wrong person. They are
never rendered as bare IDs side by side; each is labelled with its role, and the
naming in Go does not inherit Synapse's (`Reporter` and `Sender`, not `UserID`).

## Scope

**Ships:** the report queue (list, paging, open/handled filter), report detail with
the reported event's content and links into the existing room detail, and
mark-handled / mark-dismissed / reopen with a note.

**Does not ship: media quarantine.** It is the other half of the roadmap line and it
is a genuinely separate piece of work — the reported event has to be parsed for media
references, `room/<id>/media` has to be reconciled with them, and the quarantine
endpoints are POST-with-inverse. Bolting it onto the end of this etappe would give it
a fraction of the attention the reversible-pair reasoning in §4.39 deserves. It gets
its own etappe, as room deletion did.

**Does not ship: deleting reports from Synapse.** Covered above — it is the primitive
this etappe deliberately does not build on.

## Definition of done

- The queue lists real reports from the live server, with paging
- Reporter and reported sender are labelled, never two bare IDs
- A report can be marked handled and un-marked, and Synapse's record is unchanged
  either way — verified by reading the report back from Synapse after marking
- A report links to its room's existing detail page
- An empty queue says "no open reports", not "could not load"
- The expired-token path shows the connect panel, not a signed-out session
- `make check` green, live test against the real Synapse
